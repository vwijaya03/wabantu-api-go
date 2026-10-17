package salesbrain

import (
	"fmt"
	"strings"
)

// ApplyItemOps sets the desired quantity per catalog row (qty 0 removes the line). Unknown row
// numbers are reported, not applied. changed is false when nothing effectively changed.
func ApplyItemOps(cart []OrderLine, ops []ItemOp, catalog []CatalogFact) ([]OrderLine, bool, []int) {
	byNo := make(map[int]CatalogFact, len(catalog))
	for _, c := range catalog {
		byNo[c.No] = c
	}
	out := append([]OrderLine(nil), cart...)
	changed := false
	var unknown []int
	for _, op := range ops {
		c, ok := byNo[op.No]
		if !ok {
			unknown = append(unknown, op.No)
			continue
		}
		idx := -1
		for i, l := range out {
			if (l.ID != "" && l.ID == c.ID) || (l.ID == "" && strings.EqualFold(l.Name, c.Name)) {
				idx = i
				break
			}
		}
		switch {
		case op.Qty == 0 && idx >= 0:
			out = append(out[:idx], out[idx+1:]...)
			changed = true
		case op.Qty > 0 && idx >= 0:
			if out[idx].Qty != op.Qty || out[idx].UnitPrice != c.Price {
				out[idx].Qty, out[idx].UnitPrice = op.Qty, c.Price
				changed = true
			}
		case op.Qty > 0:
			out = append(out, OrderLine{ID: c.ID, Name: c.Name, Qty: op.Qty, UnitPrice: c.Price})
			changed = true
		}
	}
	return out, changed, unknown
}

// CartSubtotal sums qty × unit price.
func CartSubtotal(cart []OrderLine) int64 {
	var s int64
	for _, l := range cart {
		s += int64(l.Qty) * l.UnitPrice
	}
	return s
}

// RenderCartSummary is the code-rendered order summary appended after the model's reply.
func RenderCartSummary(cart []OrderLine, lang Lang) string {
	if len(cart) == 0 {
		return ""
	}
	var b strings.Builder
	if lang == LangEN {
		b.WriteString("Order summary:\n")
	} else {
		b.WriteString("Ringkasan pesanan:\n")
	}
	for i, l := range cart {
		fmt.Fprintf(&b, "%d. %dx %s — %s\n", i+1, l.Qty, l.Name, FormatRupiah(int64(l.Qty)*l.UnitPrice))
	}
	if lang == LangEN {
		fmt.Fprintf(&b, "Subtotal: %s (shipping not included)", FormatRupiah(CartSubtotal(cart)))
	} else {
		fmt.Fprintf(&b, "Subtotal: %s (belum termasuk ongkir)", FormatRupiah(CartSubtotal(cart)))
	}
	return b.String()
}

// ResumeLineFor re-asks a pending checkout field in the buyer language.
func ResumeLineFor(field string, lang Lang) string {
	switch field {
	case "recipient":
		return T(TplResumeRecipient, lang)
	case "address":
		return T(TplResumeAddress, lang)
	case "confirm":
		return T(TplResumeConfirm, lang)
	}
	return ""
}
