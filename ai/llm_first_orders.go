package ai

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"

	bf "encore.app/wabantu/internal/buyerflow"
	"encore.app/wabantu/internal/salesbrain"
)

// Order adapter of the LLM-first pipeline: pure functions that translate between the salesbrain
// cart / field data and the shared checkout state (bf.OrderState, stored in Redis and read by
// the legacy order code). The I/O shell loads and saves the state; nothing here touches Redis
// or the database.

// Checkout steps of bf.OrderState that the adapter reads or writes. The legacy flow has no
// confirm step: it persists as soon as the shipping data is complete.
const (
	lfStepAskVariant     = "ask_variant"
	lfStepAskRecipient   = "ask_recipient"
	lfStepAskAddressFull = "ask_address_full"
	lfStepConfirm        = "confirm"
)

// cartFromState reads the checkout state as a salesbrain cart. Lines come from Items; a legacy
// single-item state (no Items) reads as a one-line cart. A product that has no quantity yet is
// not a line: quantities are never invented. Each line carries its size and colour; a line whose
// product needs a variant that is still missing is flagged VariantMissing.
func cartFromState(st *orderState) []salesbrain.OrderLine {
	if st == nil {
		return nil
	}
	var cart []salesbrain.OrderLine
	if st.HasMultiItems() {
		for _, l := range st.Items {
			name := strings.TrimSpace(l.ProductName)
			if l.Qty < 1 || (l.CatalogItemID == "" && name == "") {
				continue
			}
			cart = append(cart, lfOrderLine(l))
		}
		return cart
	}
	norm := bf.NormalizeOrderState(*st) // fills ProductName from the old Product field, Size from the name
	name := strings.TrimSpace(norm.ProductName)
	if norm.Qty < 1 || (norm.CatalogItemID == "" && name == "") {
		return nil
	}
	return []salesbrain.OrderLine{lfOrderLine(orderLineState{
		CatalogItemID: norm.CatalogItemID, ExternalCode: norm.ExternalCode, ProductName: name,
		Size: norm.Size, Color: norm.Color, Qty: norm.Qty, UnitPrice: norm.UnitPrice,
	})}
}

// lfOrderLine maps one stored line to a cart line. A size that the product name carries ("… - L")
// counts as the line's size even when the stored line lacks it.
func lfOrderLine(l orderLineState) salesbrain.OrderLine {
	name := strings.TrimSpace(l.ProductName)
	if strings.TrimSpace(l.Size) == "" {
		l.Size = bf.ExtractSizeFromProductName(name)
	}
	return salesbrain.OrderLine{
		ID: l.CatalogItemID, Name: name, Qty: l.Qty, UnitPrice: lfRoundPrice(l.UnitPrice),
		Size: strings.TrimSpace(l.Size), Color: strings.TrimSpace(l.Color),
		VariantMissing: !bf.LineVariantComplete(orderLineState{ProductName: name, ExternalCode: l.ExternalCode, Size: l.Size, Color: l.Color}),
	}
}

// stateWithCart returns a copy of st whose lines are exactly cart (nil when the cart is empty).
// ExternalCode and SellUnit come from the catalog row with the same id; the size, colour and
// warehouse of a line that already existed survive a quantity change. The first line is
// mirrored into the legacy single-item fields (as bf.ApplyLineToOrderState does everywhere in
// the legacy flow), and a step that is still before the recipient moves to ask_recipient.
// Everything else (recipient, address, draft binding, upsell flag) is kept. st may be nil.
func stateWithCart(st *orderState, cart []salesbrain.OrderLine, catalog []dbCatalogItem) *orderState {
	if len(cart) == 0 {
		return nil
	}
	var out orderState
	if st != nil {
		out = *st
	}

	// A line is its product plus its size and colour: one product can be several lines.
	type lineKey struct{ id, size, color string }
	keyOf := func(id, size, color string) lineKey {
		return lineKey{id, strings.ToLower(strings.TrimSpace(size)), strings.ToLower(strings.TrimSpace(color))}
	}
	earlier := make(map[lineKey]orderLineState, len(out.Items)+1)
	byID := make(map[string]orderLineState, len(out.Items)+1)
	idLines := make(map[string]int, len(out.Items)+1)
	remember := func(l orderLineState) {
		if l.CatalogItemID == "" {
			return
		}
		earlier[keyOf(l.CatalogItemID, l.Size, l.Color)] = l
		idLines[l.CatalogItemID]++
		if _, ok := byID[l.CatalogItemID]; !ok {
			byID[l.CatalogItemID] = l
		}
	}
	for _, l := range out.Items {
		remember(l)
	}
	if len(out.Items) == 0 && out.CatalogItemID != "" { // legacy single-item state
		remember(orderLineState{
			CatalogItemID: out.CatalogItemID, ExternalCode: out.ExternalCode, ProductName: out.ProductName,
			Size: out.Size, Color: out.Color, Qty: out.Qty, UnitPrice: out.UnitPrice, SellUnit: out.SellUnit,
			WarehouseID: out.WarehouseID,
		})
	}
	rows := make(map[string]dbCatalogItem, len(catalog))
	for _, it := range catalog {
		rows[it.ID] = it
	}

	missing := false
	lines := make([]orderLineState, 0, len(cart))
	for _, c := range cart {
		line := orderLineState{
			CatalogItemID: c.ID, ProductName: c.Name, Qty: c.Qty, UnitPrice: float64(c.UnitPrice),
			Size: c.Size, Color: c.Color,
		}
		if prev, ok := earlier[keyOf(c.ID, c.Size, c.Color)]; ok {
			line.ExternalCode, line.SellUnit, line.WarehouseID = prev.ExternalCode, prev.SellUnit, prev.WarehouseID
		} else if prev, ok := byID[c.ID]; ok {
			line.ExternalCode, line.SellUnit = prev.ExternalCode, prev.SellUnit
			if c.Size == "" && c.Color == "" && !c.VariantMissing && idLines[c.ID] == 1 {
				// A cart that names no variant for the product's only stored line keeps that line's variant.
				line.Size, line.Color, line.WarehouseID = prev.Size, prev.Color, prev.WarehouseID
			}
		}
		if row, ok := rows[c.ID]; ok && c.ID != "" {
			line.ExternalCode, line.SellUnit = row.ExternalCode, row.SellUnit
		}
		missing = missing || c.VariantMissing
		lines = append(lines, line)
	}
	out.Items = lines
	bf.ApplyLineToOrderState(&out, lines[0])

	switch {
	case missing:
		out.Step = lfStepAskVariant // nothing moves on until the size or colour is known
	case out.Step == lfStepAskVariant:
		out.Step = lfStepAfterItems(&out) // the last open variant was filled: carry on from the collected data
	case out.Step == "" || out.Step == "ask_product" || out.Step == "ask_qty":
		out.Step = lfStepAskRecipient
	}
	return &out
}

// lfStepAfterItems is the step that follows a complete cart, from the data already collected: the
// recipient, then the address, then the final confirmation.
func lfStepAfterItems(st *orderState) string {
	switch {
	case strings.TrimSpace(st.RecipientName) == "" || strings.TrimSpace(st.RecipientPhone) == "":
		return lfStepAskRecipient
	case !st.ShippingComplete():
		return lfStepAskAddressFull
	}
	return lfStepConfirm
}

// pendingFieldFromState names the checkout field the buyer still owes:
// "", "variant", "recipient", "address" or "confirm".
func pendingFieldFromState(st *orderState) string {
	if st == nil {
		return ""
	}
	if lfCartMissingVariant(cartFromState(st)) {
		return "variant"
	}
	return pendingFieldAfterVariant(st)
}

// pendingFieldAfterVariant is the pending field once the variants are settled: what follows the
// "which size?" question.
func pendingFieldAfterVariant(st *orderState) string {
	if st == nil {
		return ""
	}
	switch st.Step {
	case lfStepAskRecipient:
		return "recipient"
	case "ask_address", lfStepAskAddressFull:
		return "address"
	case lfStepConfirm:
		return "confirm"
	case lfStepAskVariant:
		if len(cartFromState(st)) > 0 {
			switch lfStepAfterItems(st) {
			case lfStepAskRecipient:
				return "recipient"
			case lfStepAskAddressFull:
				return "address"
			}
			return "confirm"
		}
	}
	return ""
}

// lfCartMissingVariant: some line still lacks the size or colour its product needs.
func lfCartMissingVariant(cart []salesbrain.OrderLine) bool {
	for _, l := range cart {
		if l.VariantMissing {
			return true
		}
	}
	return false
}

// applyFieldAnswer validates the checkout fields the model extracted from buyerText and writes
// the valid ones into st; it returns the problems it found in the buyer language, as fragments
// the caller joins into the reply (e.g. "nomor HP 98822123 belum valid (8 digit; …)").
//
//   - No invented data: a field is written only when its value also appears in buyerText,
//     compared case-insensitively (phone and postal code: digits only, aligned on the number
//     groups the buyer typed). A value that does not appear is ignored without a problem.
//   - phone: Indonesian mobile, 08… with 10-13 digits (or 628… with 11-14); stored as +62….
//   - name: bf.PlausibleNameLine, and never taken from a question unless it carries an
//     explicit "Name:" label (a delivery question once became the recipient name).
//   - postal code: exactly 5 digits. Street, city, province and district: non-empty.
//   - steps: ask_recipient moves to ask_address_full once name and phone are both set, and any
//     step moves to confirm as soon as the shipping data is complete.
//
// A grounded value overwrites the stored one (the buyer is correcting it); an empty one never
// erases anything.
func applyFieldAnswer(st *orderState, f *salesbrain.FieldData, buyerText string, lang salesbrain.Lang) (problems []string) {
	if st == nil || f == nil {
		return nil
	}

	// The deterministic parser reads the buyer's own words first; the model's grounded fields only
	// fill what the parser left empty (a mislabelled city/province once lost a whole address).
	parsed := lfParseShipping(st, f, buyerText)

	if name := strings.TrimSpace(f.Name); name != "" && lfTextContains(buyerText, name) &&
		bf.PlausibleNameLine(name) && !lfQuestionBlocksName(buyerText) {
		st.RecipientName = name
	}

	if digits := lfDigits(f.Phone); digits != "" && lfTextHasNumber(buyerText, lfPhoneVariants(digits)) {
		if phone, ok := lfValidPhone(digits); ok {
			st.RecipientPhone = phone
		} else {
			problems = append(problems, lfPhoneProblem(digits, lang))
		}
	}

	if digits := lfDigits(f.PostalCode); digits != "" && lfTextHasNumber(buyerText, []string{digits}) {
		if len(digits) == 5 {
			st.PostalCode = digits
		} else {
			problems = append(problems, lfPostalProblem(digits, lang))
		}
	}

	lfApplyModelAddress(st, f, buyerText, parsed)

	if st.Step == lfStepAskRecipient && st.RecipientName != "" && st.RecipientPhone != "" {
		st.Step = lfStepAskAddressFull
	}
	if st.ShippingComplete() && !lfCartMissingVariant(cartFromState(st)) {
		st.Step = lfStepConfirm
	}
	return problems
}

// lfParsedFields records which address fields the deterministic parser filled in this turn.
type lfParsedFields struct{ street, city, province bool }

// lfParseShipping (f: the model's fields) runs bf.MergeShippingText on a copy of st and takes over what it found: the
// recipient (name never from a question, phone only when valid), the postal code and the
// address fields. The returned set names the address fields it changed, so the model's values
// do not overwrite them.
func lfParseShipping(st *orderState, f *salesbrain.FieldData, buyerText string) lfParsedFields {
	merged := *st
	merged.Items = append([]orderLineState(nil), st.Items...)
	bf.MergeShippingText(&merged, buyerText)

	var p lfParsedFields
	if merged.RecipientName != st.RecipientName && !lfQuestionBlocksName(buyerText) && bf.PlausibleNameLine(merged.RecipientName) {
		st.RecipientName = merged.RecipientName
	}
	if merged.RecipientPhone != st.RecipientPhone {
		// Only a number the buyer typed whole (a truncated long number is not a phone).
		digits := lfDigits(merged.RecipientPhone)
		if phone, ok := lfValidPhone(digits); ok && lfTextHasNumber(buyerText, lfPhoneVariants(digits)) {
			st.RecipientPhone = phone
		}
	}
	if merged.PostalCode != st.PostalCode && len(merged.PostalCode) == 5 {
		st.PostalCode = merged.PostalCode
	}
	// The parser dumps a whole message into Street when it only hints at an address; that is kept
	// only when the model also found a street in the buyer's words.
	if strings.TrimSpace(merged.Street) == strings.TrimSpace(buyerText) {
		if ms := strings.TrimSpace(f.Street); ms == "" || !lfTextContains(buyerText, ms) {
			merged.Street = st.Street
		}
	}
	// A street cut out in front of a city hint ("ana from") counts only when it looks like one.
	if merged.Street != st.Street && !bf.OrderAddrHintRe.MatchString(merged.Street) {
		merged.Street = st.Street
	}
	take := func(dst *string, v string, flag *bool) {
		if v = strings.TrimSpace(v); v != "" && v != *dst {
			*dst, *flag = v, true
		}
	}
	take(&st.Street, merged.Street, &p.street)
	take(&st.City, merged.City, &p.city)
	take(&st.Province, merged.Province, &p.province)
	return p
}

// lfApplyModelAddress writes the model's grounded street, district, city and province. A value
// the parser set this turn is kept. A "city" that is really a province name or alias ("DKI
// Jakarta") becomes the province when that is empty, and the model's district then stands in
// for the missing city; a "district" that is the city the parser already recognised is dropped.
func lfApplyModelAddress(st *orderState, f *salesbrain.FieldData, buyerText string, parsed lfParsedFields) {
	grounded := func(v string) string {
		if v = strings.TrimSpace(v); v != "" && lfTextContains(buyerText, v) {
			return v
		}
		return ""
	}
	street, district, city, province := grounded(f.Street), grounded(f.District), grounded(f.City), grounded(f.Province)

	if canon, ok := bf.CanonicalProvince(city); ok {
		// The model put a province name in the city slot.
		if province == "" && strings.TrimSpace(st.Province) == "" {
			province = canon
		}
		city = ""
		if district != "" && strings.TrimSpace(st.City) == "" {
			city, district = district, ""
		}
	}
	if district != "" {
		if known, ok := bf.KnownCityHint(district); ok && strings.EqualFold(known, strings.TrimSpace(st.City)) {
			district = ""
		}
	}

	// The parser may leave postal code or province glued to the street; the model's own street
	// is then the cleaner value.
	if strings.TrimSpace(st.Street) != "" && street != "" && parsed.street &&
		len(street) < len(st.Street) && strings.Contains(strings.ToLower(st.Street), strings.ToLower(street)) {
		parsed.street = false
	}

	for _, field := range []struct {
		dst    *string
		value  string
		parsed bool
	}{
		{&st.Street, street, parsed.street},
		{&st.Kecamatan, district, false},
		{&st.City, city, parsed.city},
		{&st.Province, province, parsed.province},
	} {
		if field.value != "" && !field.parsed {
			*field.dst = field.value
		}
	}
}

func lfRoundPrice(p float64) int64 { return int64(math.Round(p)) }

// lfNormText lowercases s and reduces it to space-separated letter/digit words, so "Jl. Mawar
// No.5,\nSurabaya" and "jl mawar no 5 surabaya" compare equal.
func lfNormText(s string) string {
	var b strings.Builder
	gap := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if gap && b.Len() > 0 {
				b.WriteByte(' ')
			}
			gap = false
			b.WriteRune(r)
			continue
		}
		gap = true
	}
	return b.String()
}

// lfTextContains reports whether value appears in text as whole words, ignoring case and
// punctuation.
func lfTextContains(text, value string) bool {
	v := lfNormText(value)
	return v != "" && strings.Contains(" "+lfNormText(text)+" ", " "+v+" ")
}

// lfQuestionBlocksName mirrors the legacy rule in bf.ParseRecipientLine: a question mid-checkout
// is never a recipient name, unless the same message carries an explicit "Name:" label (in
// which case ParseRecipientLine still returns that name).
func lfQuestionBlocksName(buyerText string) bool {
	if !bf.LooksLikeBuyerQuestion(buyerText) {
		return false
	}
	labelled, _ := bf.ParseRecipientLine(buyerText)
	return labelled == ""
}

var (
	// lfNumberRunRe finds a number as a buyer types it: digits that may be separated by spaces,
	// dashes, dots or parentheses, with an optional leading +.
	lfNumberRunRe = regexp.MustCompile(`\+?\d[\d ().\-]*\d|\d`)
	lfDigitRunRe  = regexp.MustCompile(`\d+`)
)

func lfDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// lfPhoneVariants returns the digit strings the same phone may be typed as: as given, in local
// form (08…) and in international form (628…).
func lfPhoneVariants(digits string) []string {
	variants := []string{digits}
	switch {
	case strings.HasPrefix(digits, "62") && len(digits) > 2:
		variants = append(variants, "0"+digits[2:])
	case strings.HasPrefix(digits, "0") && len(digits) > 1:
		variants = append(variants, "62"+digits[1:])
	}
	return variants
}

// lfTextHasNumber reports whether one of variants appears in text as a whole number. A number
// typed in groups ("0872 8394 72828") counts as one number, but a value carved out of the
// middle of a longer number, or spanning two separate numbers, does not.
func lfTextHasNumber(text string, variants []string) bool {
	maxLen := 0
	for _, v := range variants {
		maxLen = max(maxLen, len(v))
	}
	for _, run := range lfNumberRunRe.FindAllString(text, -1) {
		groups := lfDigitRunRe.FindAllString(run, -1)
		for i := range groups {
			joined := ""
			for _, g := range groups[i:] {
				joined += g
				if len(joined) > maxLen {
					break // keeps a long "1 1 1 1 …" message linear
				}
				for _, v := range variants {
					if joined == v {
						return true
					}
				}
			}
		}
	}
	return false
}

// lfValidPhone normalises an Indonesian mobile number to +62… and checks its length: 08… with
// 10-13 digits, which is 628… with 11-14 digits.
func lfValidPhone(digits string) (string, bool) {
	phone := bf.NormalizePhoneID(digits)
	d := lfDigits(phone)
	if strings.HasPrefix(d, "628") && len(d) >= 11 && len(d) <= 14 {
		return phone, true
	}
	return "", false
}

func lfPhoneProblem(digits string, lang salesbrain.Lang) string {
	if lang == salesbrain.LangEN {
		return fmt.Sprintf("the phone number %s has %d digits, which doesn't look valid (it should start with 08 and have 10-13 digits)", digits, len(digits))
	}
	return fmt.Sprintf("nomor HP %s belum valid (%d digit; seharusnya diawali 08 dan terdiri dari 10-13 digit)", digits, len(digits))
}

func lfPostalProblem(digits string, lang salesbrain.Lang) string {
	if lang == salesbrain.LangEN {
		return fmt.Sprintf("the postal code %s has %d digits but should have 5", digits, len(digits))
	}
	return fmt.Sprintf("kode pos %s belum valid (%d digit; seharusnya 5 digit)", digits, len(digits))
}
