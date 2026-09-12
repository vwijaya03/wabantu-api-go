package triageassert

import (
	"fmt"

	"encore.app/wabantu/shared/kbcontext"
)

// RetrievalSpec asserts ordered KB/catalog IDs and fallback mode.
type RetrievalSpec struct {
	KBEntryIDs      []string
	CatalogIDs      []string
	Mode            string
	DegradedMode    kbcontext.DegradedMode
	AllowEmptyIndex bool
}

// CheckTrace compares a frozen retrieval trace to the spec.
func CheckTrace(tr kbcontext.Trace, spec RetrievalSpec) error {
	if spec.Mode != "" && tr.Mode != spec.Mode && spec.DegradedMode == "" {
		return fmt.Errorf("retrieval mode=%q want %q", tr.Mode, spec.Mode)
	}
	if spec.DegradedMode != "" && tr.DegradedMode != spec.DegradedMode {
		return fmt.Errorf("degradedMode=%q want %q", tr.DegradedMode, spec.DegradedMode)
	}
	if !equalIDs(tr.KBEntryIDs, spec.KBEntryIDs) {
		return fmt.Errorf("kb ids=%v want %v", tr.KBEntryIDs, spec.KBEntryIDs)
	}
	if !equalIDs(tr.CatalogIDs, spec.CatalogIDs) {
		return fmt.Errorf("catalog ids=%v want %v", tr.CatalogIDs, spec.CatalogIDs)
	}
	if !spec.AllowEmptyIndex && spec.DegradedMode == "" && len(spec.KBEntryIDs) == 0 && len(spec.CatalogIDs) == 0 && len(tr.KBEntryIDs) == 0 && len(tr.CatalogIDs) == 0 && tr.LexicalFallback {
		return fmt.Errorf("empty retrieval without allowEmptyIndex")
	}
	return nil
}

func equalIDs(a, b []string) bool {
	if b == nil {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
