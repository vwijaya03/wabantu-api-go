package order

import (
	"strings"
	"testing"
)

func TestOrderContactSearchSQLOrderRef(t *testing.T) {
	frag, args := orderContactSearchSQL(1, "WB-58D662BC", false)
	if !strings.Contains(frag, `UPPER(REPLACE(o.id::text, '-', '')) LIKE $2`) {
		t.Fatalf("expected order ref UUID prefix match in fragment, got %q", frag)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d: %v", len(args), args)
	}
	if args[0] != "%WB-58D662BC%" {
		t.Fatalf("first arg = %v, want %%WB-58D662BC%%", args[0])
	}
	if args[1] != "58D662BC%" {
		t.Fatalf("second arg = %v, want 58D662BC%%", args[1])
	}
}

func TestOrderContactSearchSQLPlainQuery(t *testing.T) {
	frag, args := orderContactSearchSQL(1, "John", false)
	if strings.Contains(frag, `UPPER(REPLACE(o.id::text, '-', ''))`) {
		t.Fatalf("plain name search should not add order ref match, got %q", frag)
	}
	if len(args) != 1 || args[0] != "%John%" {
		t.Fatalf("expected single like arg, got %v", args)
	}
}
