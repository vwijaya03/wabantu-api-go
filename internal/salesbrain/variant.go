package salesbrain

import (
	"fmt"
	"regexp"
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
	BuyerTexts []string // current + recent buyer messages: a variant must appear in them
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
func ResolveCartOps(in ResolveInput) CartResolution {
	byNo := make(map[int]CatalogFact, len(in.Catalog))
	for _, c := range in.Catalog {
		byNo[c.No] = c
	}
	res := CartResolution{Cart: append([]OrderLine(nil), in.Cart...)}

	for _, op := range in.Ops {
		c, ok := byNo[op.No]
		if !ok {
			res.Unknown = append(res.Unknown, op.No)
			continue
		}
		size, color := grounded(in.BuyerTexts, op.Size), grounded(in.BuyerTexts, op.Color)

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

		matches := lineMatches(res.Cart, c)
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
			res.Issues = append(res.Issues, OpIssue{Kind: IssueVariantMissing, No: c.No, Name: displayName(c)})
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
			if op.Qty > line.Qty {
				if is := stockIssue(in.Catalog, c, op.Qty); is != nil {
					res.Issues = append(res.Issues, *is)
					continue
				}
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
			if c.Size != "" && len(Siblings(in.Catalog, c)) > 0 && !groundedWord(in.BuyerTexts, c.Size) {
				// The buyer never said which size: nothing is added, the choices are asked.
				res.Issues = append(res.Issues, OpIssue{Kind: IssueChooseVariant, No: c.No, Name: displayName(c), Options: sizesInStock(in.Catalog, c)})
				continue
			}
			if is := stockIssue(in.Catalog, c, op.Qty); is != nil {
				res.Issues = append(res.Issues, *is)
				continue
			}
			line := OrderLine{ID: c.ID, Name: c.Name, Qty: op.Qty, UnitPrice: c.Price, Size: size, Color: color}
			line.VariantMissing = c.NeedsVariant && size == "" && color == ""
			if line.VariantMissing {
				res.Issues = append(res.Issues, OpIssue{Kind: IssueVariantMissing, No: c.No, Name: displayName(c)})
			}
			res.Cart = append(res.Cart, line)
			res.Changed = true
		}
	}
	return res
}

// stockIssue refuses a quantity the row cannot supply. A row without stock data (Stock "") is
// never refused here; the checkout save keeps its own stock guard.
func stockIssue(catalog []CatalogFact, c CatalogFact, qty int) *OpIssue {
	switch {
	case c.Stock == StockOut:
		return &OpIssue{Kind: IssueSoldOut, No: c.No, Name: c.Name, Options: sizesInStock(catalog, c)}
	case c.Stock != "" && int(c.StockQty) > 0 && qty > int(c.StockQty):
		return &OpIssue{Kind: IssueStockLimit, No: c.No, Name: c.Name, Available: int(c.StockQty)}
	}
	return nil
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

func lineMatches(cart []OrderLine, c CatalogFact) []int {
	var idx []int
	for i, l := range cart {
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
