package salesbrain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Variant-aware cart operations (sizes and colours).
//
// A catalog row is either a plain product, a sized SKU (the size is part of its name, "… - L", and
// the other sizes are sibling rows that share the same BaseName) or a product that needs a size
// and/or colour the buyer must name (a T-shirt row without a size in its name). The model proposes
// the variant in ItemOp.Size / ItemOp.Color; code accepts a variant only when the buyer typed it
// (the same "no invented data" rule as the checkout fields), checks stock and price against the
// catalog row, and answers every refusal with a template, so the buyer always hears the truth in
// the locked language.

// Stock labels of CatalogFact.Stock.
const (
	StockIn  = "tersedia"
	StockOut = "habis"
)

// IssueKind classifies an op that could not be applied as the model proposed.
type IssueKind string

const (
	// IssueVariantMissing: the product needs a size or colour the buyer has not given. The line is
	// kept (with VariantMissing) so the quantity is not lost; the buyer is asked once.
	IssueVariantMissing IssueKind = "variant_missing"
	// IssueChooseVariant: the buyer did not name which sibling size they want. Nothing is added.
	IssueChooseVariant IssueKind = "choose_variant"
	// IssueVariantUnknown: the buyer asked for a size the product does not come in.
	IssueVariantUnknown IssueKind = "variant_unknown"
	// IssueColorUnknown: the buyer named a colour this product does not sell.
	IssueColorUnknown IssueKind = "color_unknown"
	// IssueSoldOut: the row (or variant) has no stock. Options lists the sibling sizes in stock.
	IssueSoldOut IssueKind = "sold_out"
	// IssueStockLimit: more units wanted than the stock holds. Available is what is left.
	IssueStockLimit IssueKind = "stock_limit"
)

// OpIssue is one refused or incomplete op, rendered by RenderIssue.
type OpIssue struct {
	Kind      IssueKind
	No        int
	Name      string
	Options   []string // sizes the buyer may pick (in stock)
	Available int      // IssueStockLimit only
}

// ResolveInput is everything ResolveCartOps needs.
type ResolveInput struct {
	Cart       []OrderLine
	Ops        []ItemOp
	Catalog    []CatalogFact
	BuyerTexts []string // current + recent buyer messages (phones, amounts in Validate)
	// VariantTexts grounds size/colour only from these messages (usually the current turn only),
	// so a house number in an earlier address message is never read as a size.
	VariantTexts []string
}

// CartResolution is the outcome of ResolveCartOps. Cart is a new slice; the input is never mutated.
type CartResolution struct {
	Cart    []OrderLine
	Changed bool
	Unknown []int // op numbers that match no catalog row
	Issues  []OpIssue
}

// Siblings returns the other rows of the same product in another size: same BaseName, both rows
// sized. Rows without a size have no siblings.
func Siblings(catalog []CatalogFact, c CatalogFact) []CatalogFact {
	if c.Size == "" || strings.TrimSpace(c.BaseName) == "" {
		return nil
	}
	var out []CatalogFact
	for _, o := range catalog {
		if o.No == c.No || o.Size == "" || !strings.EqualFold(o.BaseName, c.BaseName) {
			continue
		}
		out = append(out, o)
	}
	return out
}

// ResolveCartOps applies the model's item ops to the cart with the variant, stock and price rules
// above. Prices always come from the catalog row, never from the model. For catalogs without
// variants and stock data it behaves like ApplyItemOps.
func variantTexts(in ResolveInput) []string {
	if len(in.VariantTexts) > 0 {
		return in.VariantTexts
	}
	return in.BuyerTexts
}

func ResolveCartOps(in ResolveInput) CartResolution {
	byNo := make(map[int]CatalogFact, len(in.Catalog))
	for _, c := range in.Catalog {
		byNo[c.No] = c
	}
	res := CartResolution{Cart: append([]OrderLine(nil), in.Cart...)}
	vTexts := variantTexts(in)

	for _, op := range in.Ops {
		c, ok := byNo[op.No]
		if !ok {
			res.Unknown = append(res.Unknown, op.No)
			continue
		}
		size, color := grounded(vTexts, op.Size), grounded(vTexts, op.Color)

		// The buyer named another size of a sized row: that is the sibling row, when it exists.
		if c.Size != "" && size != "" && !strings.EqualFold(size, c.Size) {
			sib, found := siblingWithSize(in.Catalog, c, size)
			if !found {
				res.Issues = append(res.Issues, OpIssue{Kind: IssueVariantUnknown, No: c.No, Name: displayName(c), Options: sizesInStock(in.Catalog, c)})
				continue
			}
			c = sib
		}
		switch {
		case c.Size != "":
			size = c.Size // the row's own size is the size of the line
		case !c.NeedsVariant:
			size, color = "", "" // a plain product has no variant
		}
		if color != "" && c.NeedsVariant {
			if sib, found := siblingWithColor(in.Catalog, c, color); found {
				c = sib
			}
		}
		if color != "" && c.NeedsVariant && !colorAllowed(in.Catalog, c, color) {
			res.Issues = append(res.Issues, OpIssue{
				Kind: IssueColorUnknown, No: c.No, Name: displayName(c), Options: soldColorsForProduct(in.Catalog, c),
			})
			continue
		}

		matches := lineMatches(res.Cart, in.Catalog, c)
		idx := -1
		switch {
		case size != "" || color != "":
			idx = findVariantLine(res.Cart, matches, size, color)
		case len(matches) == 1:
			idx = matches[0]
		case len(matches) > 1:
			if op.Qty == 0 { // "remove it": every variant of the product goes
				res.Cart = removeLines(res.Cart, matches)
				res.Changed = true
				continue
			}
			res.Issues = append(res.Issues, OpIssue{Kind: IssueVariantMissing, No: c.No, Name: displayName(c), Options: variantChoices(in.Catalog, c)})
			continue
		}

		switch {
		case op.Qty == 0:
			if idx >= 0 {
				res.Cart = removeLines(res.Cart, []int{idx})
				res.Changed = true
			}

		case idx >= 0:
			line := &res.Cart[idx]
			if is := stockIssueForLine(in.Catalog, res.Cart, c, op.Qty, idx); is != nil {
				res.Issues = append(res.Issues, *is)
				continue
			}
			before := *line
			line.Qty, line.UnitPrice = op.Qty, c.Price
			if size != "" || color != "" {
				line.Size, line.Color = size, color
			}
			line.VariantMissing = c.NeedsVariant && line.Size == "" && line.Color == ""
			if *line != before {
				res.Changed = true
			}

		default:
			if c.Size != "" && len(Siblings(in.Catalog, c)) > 0 && !groundedWord(vTexts, c.Size) {
				// The buyer never said which size: nothing is added, the choices are asked.
				res.Issues = append(res.Issues, OpIssue{Kind: IssueChooseVariant, No: c.No, Name: displayName(c), Options: sizesInStock(in.Catalog, c)})
				continue
			}
			if is := stockIssueForLine(in.Catalog, res.Cart, c, op.Qty, -1); is != nil {
				res.Issues = append(res.Issues, *is)
				continue
			}
			line := OrderLine{ID: c.ID, Name: c.Name, Qty: op.Qty, UnitPrice: c.Price, Size: size, Color: color}
			line.VariantMissing = c.NeedsVariant && size == "" && color == ""
			if line.VariantMissing {
				res.Issues = append(res.Issues, OpIssue{Kind: IssueVariantMissing, No: c.No, Name: displayName(c), Options: variantChoices(in.Catalog, c)})
			}
			res.Cart = append(res.Cart, line)
			res.Changed = true
		}
	}
	return res
}

// stockPoolID is the catalog row whose StockQty caps every variant line (same ID, e.g. two colours).
func stockPoolID(c CatalogFact) string {
	return strings.TrimSpace(c.ID)
}

// poolQtyInCart sums quantities of cart lines that draw from the same stock pool as c.
func poolQtyInCart(cart []OrderLine, c CatalogFact, skipIdx int) int {
	pool := stockPoolID(c)
	if pool == "" {
		return 0
	}
	n := 0
	for i, l := range cart {
		if i == skipIdx || strings.TrimSpace(l.ID) != pool {
			continue
		}
		n += l.Qty
	}
	return n
}

// stockIssueForLine refuses when the pool would exceed StockQty after applying qty to skipIdx (-1 = new line).
func stockIssueForLine(catalog []CatalogFact, cart []OrderLine, c CatalogFact, qty int, lineIdx int) *OpIssue {
	switch {
	case c.Stock == StockOut:
		return &OpIssue{Kind: IssueSoldOut, No: c.No, Name: c.Name, Options: variantChoices(catalog, c)}
	case c.Stock != "" && c.StockQty > 0 && c.StockQty < 1:
		return &OpIssue{Kind: IssueSoldOut, No: c.No, Name: displayName(c), Options: variantChoices(catalog, c)}
	case c.Stock != "" && int(c.StockQty) >= 1:
		other := poolQtyInCart(cart, c, lineIdx)
		if other+qty > int(c.StockQty) {
			left := int(c.StockQty) - other
			if left < 0 {
				left = 0
			}
			return &OpIssue{Kind: IssueStockLimit, No: c.No, Name: displayName(c), Available: left}
		}
	}
	return nil
}

var (
	catalogSizeSuffixRe = regexp.MustCompile(`(?i)^(XS|S|M|L|XL|XXL|XXXL|3XL|4XL|5XL|\d{2,3})$`)
	colorHintInTextRe   = regexp.MustCompile(`(?i)(?:warna|color|colour)(?:\s+tersedia)?\s*[:\-]\s*([^.\n]+)`)
)

var knownRetailColors = map[string]bool{
	"hitam": true, "putih": true, "merah": true, "biru": true, "pink": true, "cream": true, "navy": true,
	"abu": true, "coklat": true, "hijau": true, "kuning": true, "ungu": true, "orange": true, "gold": true,
	"silver": true, "maroon": true, "tosca": true, "beige": true, "brown": true, "black": true, "white": true,
	"blue": true, "red": true, "green": true, "yellow": true, "grey": true, "gray": true, "purple": true,
	"nude": true, "mocha": true, "olive": true, "neon": true,
}

func productNameBase(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	if i := strings.LastIndex(trimmed, " - "); i >= 0 {
		suf := strings.TrimSpace(trimmed[i+3:])
		if suf != "" && !catalogSizeSuffixRe.MatchString(suf) {
			if w := normWords(strings.Fields(suf)[0]); knownRetailColors[w] {
				return strings.TrimSpace(trimmed[:i])
			}
		}
	}
	return trimmed
}

func catalogProductKey(c CatalogFact) string {
	if b := strings.TrimSpace(c.BaseName); b != "" {
		return normWords(b)
	}
	return normWords(productNameBase(c.Name))
}

func sameStockProduct(a, b CatalogFact) bool {
	if a.BaseName != "" && b.BaseName != "" && strings.EqualFold(strings.TrimSpace(a.BaseName), strings.TrimSpace(b.BaseName)) {
		return true
	}
	ka, kb := catalogProductKey(a), catalogProductKey(b)
	if ka != "" && ka == kb {
		return true
	}
	if a.ID != "" && b.ID != "" && a.ID == b.ID {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(a.Name), strings.TrimSpace(b.Name))
}

// soldColorsForProduct lists colours this product actually sells, from sibling SKU names and descriptions.
func soldColorsForProduct(catalog []CatalogFact, c CatalogFact) []string {
	seen := map[string]string{}
	for _, row := range catalog {
		if !sameStockProduct(row, c) {
			continue
		}
		for _, col := range colorsDeclaredOnRow(row) {
			key := normWords(col)
			if key == "" || seen[key] != "" {
				continue
			}
			seen[key] = strings.TrimSpace(col)
		}
	}
	out := make([]string, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func colorsDeclaredOnRow(row CatalogFact) []string {
	var out []string
	if col := colorSuffixFromName(row.Name, row.Size); col != "" {
		out = append(out, col)
	}
	if row.Description != "" {
		for _, m := range colorHintInTextRe.FindAllStringSubmatch(row.Description, -1) {
			if len(m) > 1 {
				out = append(out, splitDeclaredColors(m[1])...)
			}
		}
	}
	return out
}

func colorSuffixFromName(name, size string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if size != "" {
		suf := " - " + strings.TrimSpace(size)
		if len(name) > len(suf) && strings.EqualFold(name[len(name)-len(suf):], suf) {
			name = strings.TrimSpace(name[:len(name)-len(suf)])
		}
	}
	if i := strings.LastIndex(name, " - "); i >= 0 {
		suf := strings.TrimSpace(name[i+3:])
		if suf != "" && !catalogSizeSuffixRe.MatchString(suf) {
			return suf
		}
	}
	return ""
}

func splitDeclaredColors(chunk string) []string {
	chunk = strings.TrimSpace(chunk)
	if i := strings.IndexAny(chunk, ".;!?\n"); i >= 0 {
		chunk = chunk[:i]
	}
	var out []string
	for _, part := range strings.Split(chunk, ",") {
		part = strings.TrimSpace(part)
		for _, piece := range strings.Split(part, " dan ") {
			if c := parseColorPhrase(strings.TrimSpace(piece)); c != "" {
				out = append(out, c)
			}
		}
	}
	return out
}

var colorModifierWords = map[string]bool{
	"muda": true, "tua": true, "gelap": true, "terang": true, "neon": true,
}

var colorPhraseStopWords = map[string]bool{
	"bahan": true, "katun": true, "cotton": true, "material": true, "ukuran": true, "size": true,
	"gram": true, "gr": true, "nyaman": true, "adem": true, "tidak": true, "luntur": true, "tahan": true, "anti": true,
}

// parseColorPhrase reads a merchant-listed colour phrase (from descriptions), stopping at material words.
func parseColorPhrase(s string) string {
	words := strings.Fields(strings.TrimSpace(s))
	if len(words) == 0 {
		return ""
	}
	var out []string
	for _, w := range words {
		lw := normWords(w)
		if colorPhraseStopWords[lw] {
			break
		}
		if len(out) == 0 {
			if !knownRetailColors[lw] && !merchantColorWord(lw) {
				return ""
			}
			out = append(out, w)
			continue
		}
		if knownRetailColors[lw] || colorModifierWords[lw] || merchantColorWord(lw) {
			out = append(out, w)
			continue
		}
		break
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, " ")
}

func merchantColorWord(tok string) bool {
	if tok == "" || colorPhraseStopWords[tok] {
		return false
	}
	for _, r := range tok {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return len(tok) >= 2 && len(tok) <= 24
}

func colorsEquivalent(want, sold string) bool {
	return normWords(want) == normWords(sold)
}

func rowDeclaresColor(row CatalogFact, want string) bool {
	for _, col := range colorsDeclaredOnRow(row) {
		if colorsEquivalent(want, col) {
			return true
		}
	}
	return false
}

func siblingWithColor(catalog []CatalogFact, c CatalogFact, color string) (CatalogFact, bool) {
	if rowDeclaresColor(c, color) {
		return c, true
	}
	for _, row := range catalog {
		if row.No == c.No || !sameStockProduct(row, c) {
			continue
		}
		if rowDeclaresColor(row, color) {
			return row, true
		}
	}
	return CatalogFact{}, false
}

// colorAllowed accepts a grounded colour only when it matches a colour the catalog sells for this product.
func colorAllowed(catalog []CatalogFact, c CatalogFact, color string) bool {
	col := normWords(color)
	if col == "" {
		return false
	}
	sold := soldColorsForProduct(catalog, c)
	if len(sold) == 0 {
		return true
	}
	for _, s := range sold {
		if colorsEquivalent(color, s) {
			return true
		}
	}
	return false
}

// rowColor is the colour a catalog row carries in its name suffix ("Kaos Polos - Hitam"), or "".
func rowColor(c CatalogFact) string { return colorSuffixFromName(c.Name, c.Size) }

// variantLabel names the variant a row is: its size and/or its colour ("L", "Hitam", "L Hitam"),
// "" for a row that is not a variant of anything.
func variantLabel(c CatalogFact) string {
	return strings.TrimSpace(c.Size + " " + rowColor(c))
}

// variantSiblings are the other rows of the same product that are variants (another size or colour).
func variantSiblings(catalog []CatalogFact, c CatalogFact) []CatalogFact {
	var out []CatalogFact
	for _, o := range catalog {
		if o.No == c.No || variantLabel(o) == "" || !sameStockProduct(o, c) {
			continue
		}
		out = append(out, o)
	}
	return out
}

// variantChoices lists what the buyer can still pick for c's product: the label of every variant
// row (size and/or colour) that is not sold out, in catalog order, plus the colours the
// description declares when no row carries its own colour. It is what "ask once" and "sold out,
// here are the alternatives" offer, so both only name variants that exist and have stock.
func variantChoices(catalog []CatalogFact, c CatalogFact) []string {
	rows := append([]CatalogFact{c}, variantSiblings(catalog, c)...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].No < rows[j].No })
	var out []string
	seen := map[string]bool{}
	add := func(label string) {
		if k := strings.ToLower(label); label != "" && !seen[k] {
			seen[k] = true
			out = append(out, label)
		}
	}
	anyRowColor := false
	for _, r := range rows {
		if rowColor(r) != "" {
			anyRowColor = true
		}
		if r.Stock != StockOut {
			add(variantLabel(r))
		}
	}
	if !anyRowColor && c.Stock != StockOut {
		for _, col := range soldColorsForProduct(catalog, c) {
			add(col)
		}
	}
	return out
}

// sizesInStock lists the sizes of c and its siblings that still have stock (c itself included
// only when it is in stock), in catalog order.
func sizesInStock(catalog []CatalogFact, c CatalogFact) []string {
	rows := append([]CatalogFact{c}, Siblings(catalog, c)...)
	var sizes []string
	seen := map[string]bool{}
	for _, r := range rows {
		key := strings.ToUpper(r.Size)
		if r.Size == "" || r.Stock == StockOut || seen[key] {
			continue
		}
		seen[key] = true
		sizes = append(sizes, r.Size)
	}
	return sizes
}

func siblingWithSize(catalog []CatalogFact, c CatalogFact, size string) (CatalogFact, bool) {
	for _, s := range Siblings(catalog, c) {
		if strings.EqualFold(s.Size, size) {
			return s, true
		}
	}
	return CatalogFact{}, false
}

func displayName(c CatalogFact) string {
	if n := strings.TrimSpace(c.BaseName); n != "" {
		return n
	}
	return c.Name
}

func sameProduct(l OrderLine, c CatalogFact) bool {
	return (l.ID != "" && l.ID == c.ID) || (l.ID == "" && strings.EqualFold(l.Name, c.Name))
}

func catalogRowForLine(catalog []CatalogFact, l OrderLine) (CatalogFact, bool) {
	if id := strings.TrimSpace(l.ID); id != "" {
		for _, row := range catalog {
			if row.ID == id {
				return row, true
			}
		}
	}
	name := strings.TrimSpace(l.Name)
	for _, row := range catalog {
		if strings.EqualFold(strings.TrimSpace(row.Name), name) {
			return row, true
		}
	}
	return CatalogFact{}, false
}

func lineMatches(cart []OrderLine, catalog []CatalogFact, c CatalogFact) []int {
	var idx []int
	for i, l := range cart {
		if row, ok := catalogRowForLine(catalog, l); ok && sameStockProduct(row, c) {
			idx = append(idx, i)
			continue
		}
		if sameProduct(l, c) {
			idx = append(idx, i)
		}
	}
	return idx
}

// findVariantLine finds the line with exactly this size and colour among the product's lines; with
// none, the product's open line (variant still missing) is the one the buyer is now filling in.
func findVariantLine(cart []OrderLine, matches []int, size, color string) int {
	for _, i := range matches {
		if strings.EqualFold(cart[i].Size, size) && strings.EqualFold(cart[i].Color, color) {
			return i
		}
	}
	for _, i := range matches {
		if cart[i].VariantMissing {
			return i
		}
	}
	return -1
}

func removeLines(cart []OrderLine, idx []int) []OrderLine {
	drop := make(map[int]bool, len(idx))
	for _, i := range idx {
		drop[i] = true
	}
	out := cart[:0:0]
	for i, l := range cart {
		if !drop[i] {
			out = append(out, l)
		}
	}
	return out
}

// ── grounding ────────────────────────────────────────────────────────────────

// grounded returns v when it appears as whole words in one of the buyer's texts, otherwise "".
func grounded(texts []string, v string) string {
	v = strings.TrimSpace(v)
	if groundedWord(texts, v) {
		return v
	}
	return ""
}

func groundedWord(texts []string, v string) bool {
	want := normWords(v)
	if want == "" {
		return false
	}
	for _, t := range texts {
		if strings.Contains(" "+normWords(t)+" ", " "+want+" ") {
			return true
		}
	}
	return false
}

// normWords lower-cases s and reduces everything that is not a letter or digit to single spaces.
func normWords(s string) string {
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// ── rendering ────────────────────────────────────────────────────────────────

// LineLabel is the name of a cart line with its variant: "Kaos Polos (ukuran M, warna hitam)". A
// size that the product name already ends with ("… - L") is not repeated, and a line whose
// variant is still missing says so.
func LineLabel(l OrderLine, lang Lang) string {
	sizeWord, colorWord, missing := "ukuran", "warna", "ukuran/warna belum dipilih"
	if lang == LangEN {
		sizeWord, colorWord, missing = "size", "color", "size/color not chosen yet"
	}
	var parts []string
	if size := strings.TrimSpace(l.Size); size != "" && !nameEndsWithSize(l.Name, size) {
		parts = append(parts, sizeWord+" "+size)
	}
	if color := strings.TrimSpace(l.Color); color != "" {
		parts = append(parts, colorWord+" "+color)
	}
	if l.VariantMissing && len(parts) == 0 {
		parts = append(parts, missing)
	}
	if len(parts) == 0 {
		return l.Name
	}
	return fmt.Sprintf("%s (%s)", l.Name, strings.Join(parts, ", "))
}

func nameEndsWithSize(name, size string) bool {
	re := regexp.MustCompile(`(?i)[-\s]\s*` + regexp.QuoteMeta(size) + `\s*$`)
	return re.MatchString(strings.TrimSpace(name))
}

// RenderIssue is the buyer-facing sentence for one issue, from templates in the locked language.
func RenderIssue(is OpIssue, lang Lang) string {
	choices := strings.Join(is.Options, ", ")
	switch is.Kind {
	case IssueVariantMissing:
		if choices != "" {
			return T(TplAskVariantOptions, lang, is.Name, choices)
		}
		return T(TplAskVariant, lang, is.Name)
	case IssueChooseVariant:
		if choices == "" {
			return T(TplAskVariant, lang, is.Name)
		}
		return T(TplAskVariantChoices, lang, is.Name, choices)
	case IssueVariantUnknown:
		if choices == "" {
			return T(TplAskVariant, lang, is.Name)
		}
		return T(TplVariantNotFound, lang, is.Name, choices)
	case IssueColorUnknown:
		if choices == "" {
			return T(TplAskVariant, lang, is.Name)
		}
		return T(TplColorNotFound, lang, is.Name, choices)
	case IssueSoldOut:
		alt := ""
		if choices != "" {
			alt = T(TplVariantAlternatives, lang, choices)
		}
		return T(TplVariantSoldOut, lang, is.Name, alt)
	case IssueStockLimit:
		return T(TplStockLimit, lang, is.Name, is.Available)
	}
	return ""
}

// AskVariantReply is the one question for a cart line whose size or colour is still open, listing
// the variants of its product that exist and have stock (the generic question when none is known).
func AskVariantReply(catalog []CatalogFact, l OrderLine, name string, lang Lang) string {
	is := OpIssue{Kind: IssueVariantMissing, Name: name}
	if row, ok := catalogRowForLine(catalog, l); ok {
		is.No, is.Options = row.No, variantChoices(catalog, row)
	}
	return RenderIssue(is, lang)
}

// RenderIssues joins the sentences of all issues, one paragraph each.
func RenderIssues(issues []OpIssue, lang Lang) string {
	var parts []string
	for _, is := range issues {
		if s := RenderIssue(is, lang); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}
