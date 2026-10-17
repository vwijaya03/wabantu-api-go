package salesbrain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CatalogFact is one catalog row as shown to the model (No is the 1-based row number).
type CatalogFact struct {
	No          int
	Name        string
	Price       int64 // price for this buyer's tier
	Unit        string
	Stock       string // "" when stock is not tracked
	Description string
	Code        string // internal external_code; never shown to the model or the buyer
}

// KBFact is one knowledge-base row.
type KBFact struct {
	Question string
	Answer   string
	Category string
}

// FactsInput gathers everything a reply may legitimately quote.
type FactsInput struct {
	Catalog      []CatalogFact
	KB           []KBFact
	Cart         []OrderLine // cart AFTER applying this turn's item ops
	CartSubtotal int64
	OpenOrders   []OrderSnapshot
	BuyerTexts   []string // current + recent buyer messages (their own numbers may be echoed)
}

// Facts is the allow-list used by Validate.
type Facts struct {
	Amounts       map[int64]bool
	Numbers       map[string]bool // digit strings (≥ 8 digits) the reply may contain
	InternalCodes []string
}

// Violation codes.
const (
	VEmpty        = "empty_reply"
	VTooLong      = "too_long"
	VAmount       = "amount_not_in_facts"
	VNumber       = "number_not_in_facts"
	VInternalCode = "internal_code"
	VLanguage     = "language_mismatch"
)

// MaxReplyRunes caps one WhatsApp reply.
const MaxReplyRunes = 700

// Violation is one reason a model reply cannot be sent as is.
type Violation struct {
	Code   string
	Detail string
}

const maxQtyMultiple = 50

// BuildFacts derives the allow-list: catalog prices and qty×price (1..50), cart lines and
// subtotal, open-order totals, amounts and long numbers in KB answers, and numbers the buyer
// typed (phones are also allowed in 0…/62… form).
func BuildFacts(in FactsInput) Facts {
	f := Facts{Amounts: map[int64]bool{}, Numbers: map[string]bool{}}
	for _, c := range in.Catalog {
		if c.Price > 0 {
			for q := int64(1); q <= maxQtyMultiple; q++ {
				f.Amounts[c.Price*q] = true
			}
		}
		if c.Code != "" {
			f.InternalCodes = append(f.InternalCodes, c.Code)
		}
	}
	addLines := func(lines []OrderLine) {
		for _, l := range lines {
			f.Amounts[l.UnitPrice] = true
			f.Amounts[l.UnitPrice*int64(l.Qty)] = true
		}
	}
	addLines(in.Cart)
	if in.CartSubtotal > 0 {
		f.Amounts[in.CartSubtotal] = true
	}
	for _, o := range in.OpenOrders {
		addLines(o.Lines)
		if o.Subtotal > 0 {
			f.Amounts[o.Subtotal] = true
		}
	}
	for _, k := range in.KB {
		for _, a := range extractAmounts(k.Answer) {
			f.Amounts[a] = true
		}
		for _, n := range extractLongNumbers(k.Answer) {
			f.Numbers[n] = true
		}
	}
	for _, t := range in.BuyerTexts {
		for _, a := range extractAmounts(t) {
			f.Amounts[a] = true
		}
		for _, n := range extractLongNumbers(t) {
			f.Numbers[n] = true
			switch {
			case strings.HasPrefix(n, "62"):
				f.Numbers["0"+n[2:]] = true
			case strings.HasPrefix(n, "0"):
				f.Numbers["62"+n[1:]] = true
			}
		}
	}
	return f
}

// Validate checks a decision before sending: non-empty, ≤ MaxReplyRunes, every Rp amount and
// every long number grounded in Facts, no internal codes, and the reply language equal to lang
// whenever the reply is clearly in one language.
func Validate(d Decision, f Facts, lang Lang) []Violation {
	reply := strings.TrimSpace(d.Reply)
	if reply == "" {
		return []Violation{{Code: VEmpty}}
	}
	var vs []Violation
	if n := utf8.RuneCountInString(reply); n > MaxReplyRunes {
		vs = append(vs, Violation{Code: VTooLong, Detail: strconv.Itoa(n)})
	}
	for _, a := range extractAmounts(reply) {
		if !f.Amounts[a] {
			vs = append(vs, Violation{Code: VAmount, Detail: FormatRupiah(a)})
		}
	}
	for _, n := range extractLongNumbers(reply) {
		if !f.Numbers[n] {
			vs = append(vs, Violation{Code: VNumber, Detail: n})
		}
	}
	low := strings.ToLower(reply)
	for _, c := range f.InternalCodes {
		if len(c) >= 5 && strings.Contains(low, strings.ToLower(c)) {
			vs = append(vs, Violation{Code: VInternalCode, Detail: c})
			break
		}
	}
	if got, ok := ClearLanguage(reply); ok && got != lang {
		vs = append(vs, Violation{Code: VLanguage, Detail: string(got)})
	}
	return vs
}

// ViolationNote is appended to the prompt for the single retry so the model can fix its reply.
func ViolationNote(vs []Violation) string {
	if len(vs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Balasan sebelumnya ditolak sistem. Perbaiki lalu kirim ulang JSON lengkap:\n")
	for _, v := range vs {
		switch v.Code {
		case VAmount:
			fmt.Fprintf(&b, "- Nominal %s tidak ada di KATALOG/FAQ/STATE. Jangan menghitung sendiri.\n", v.Detail)
		case VNumber:
			fmt.Fprintf(&b, "- Nomor %s tidak ada di FAQ atau pesan pembeli. Jangan mengarang nomor.\n", v.Detail)
		case VInternalCode:
			b.WriteString("- Jangan menyebut kode internal produk.\n")
		case VLanguage:
			b.WriteString("- Bahasa balasan salah. Pakai STATE.bahasa.\n")
		case VTooLong:
			b.WriteString("- Terlalu panjang. Maksimal 4 kalimat.\n")
		case VEmpty:
			b.WriteString("- reply kosong.\n")
		}
	}
	return b.String()
}

var (
	rpAmountRe   = regexp.MustCompile(`(?i)\brp\.?\s*([0-9][0-9.,]*)`)
	longNumberRe = regexp.MustCompile(`\+?\d[\d \-]{6,}\d`)
)

func extractAmounts(s string) []int64 {
	var out []int64
	for _, m := range rpAmountRe.FindAllStringSubmatch(s, -1) {
		if v, ok := parseRupiahDigits(m[1]); ok {
			out = append(out, v)
		}
	}
	return out
}

func parseRupiahDigits(s string) (int64, bool) {
	s = strings.TrimRight(s, ".,")
	if i := strings.LastIndexAny(s, ".,"); i >= 0 && len(s)-i-1 == 2 && strings.ContainsAny(s[:i], ".,") {
		s = s[:i] // drop decimals like ",00"
	}
	digits := strings.NewReplacer(".", "", ",", "").Replace(s)
	if digits == "" || len(digits) > 15 {
		return 0, false
	}
	v, err := strconv.ParseInt(digits, 10, 64)
	return v, err == nil
}

func extractLongNumbers(s string) []string {
	var out []string
	for _, m := range longNumberRe.FindAllString(s, -1) {
		var b strings.Builder
		for _, r := range m {
			if r >= '0' && r <= '9' {
				b.WriteRune(r)
			}
		}
		if d := b.String(); len(d) >= 8 {
			out = append(out, d)
		}
	}
	return out
}
