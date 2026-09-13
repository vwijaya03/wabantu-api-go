package triageassert

import (
	"fmt"
	"strings"

	bf "encore.app/wabantu/internal/buyerflow"
)

// CartItem is a catalog line the contract requires.
type CartItem struct {
	CatalogItemID string
	ExternalCode  string
	NameContains  string
	Qty           int
}

// CommerceSpec is a declarative cart assertion.
type CommerceSpec struct {
	WantPath     string
	WantStep     string
	Include      []CartItem
	ExcludeNames []string
	WantSubstr   []string
	WantNot      []string
}

// CheckTurn evaluates a simulator outcome against CommerceSpec.
func CheckTurn(out bf.TurnOutcome, spec CommerceSpec) error {
	if spec.WantPath != "" && out.Path != spec.WantPath {
		return fmt.Errorf("path=%q want %q", out.Path, spec.WantPath)
	}
	reply := out.Reply
	for _, s := range spec.WantSubstr {
		if s != "" && !strings.Contains(strings.ToLower(reply), strings.ToLower(s)) {
			return fmt.Errorf("reply missing %q", s)
		}
	}
	for _, s := range spec.WantNot {
		if s != "" && strings.Contains(strings.ToLower(reply), strings.ToLower(s)) {
			return fmt.Errorf("reply contains forbidden %q", s)
		}
	}
	if out.Order == nil {
		if len(spec.Include) > 0 {
			return fmt.Errorf("order is nil, want items")
		}
		return nil
	}
	if spec.WantStep != "" && out.Order.Step != spec.WantStep {
		return fmt.Errorf("order step=%q want %s", out.Order.Step, spec.WantStep)
	}
	items := out.Order.Items
	for _, want := range spec.Include {
		if !cartHas(items, want) {
			return fmt.Errorf("missing cart item %#v", want)
		}
	}
	for _, name := range spec.ExcludeNames {
		if cartHasName(items, name) {
			return fmt.Errorf("cart still has forbidden %q", name)
		}
	}
	return nil
}

func cartHas(items []bf.OrderLineState, want CartItem) bool {
	for _, it := range items {
		if want.CatalogItemID != "" && it.CatalogItemID != want.CatalogItemID {
			continue
		}
		if want.ExternalCode != "" && it.ExternalCode != want.ExternalCode {
			continue
		}
		if want.NameContains != "" && !strings.Contains(strings.ToLower(it.ProductName), strings.ToLower(want.NameContains)) {
			continue
		}
		if want.Qty > 0 && it.Qty != want.Qty {
			continue
		}
		return true
	}
	return false
}

func cartHasName(items []bf.OrderLineState, name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.ProductName), name) {
			return true
		}
	}
	return false
}
