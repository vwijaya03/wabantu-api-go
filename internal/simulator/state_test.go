package simulator

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	appdb "encore.app/wabantu/shared/db"
)

type scanFn func(dest ...any) error

func (f scanFn) Scan(dest ...any) error { return f(dest...) }

// tenantScopeQuerier mimics appdb.TenantScope: every query goes through QualifySQL.
func tenantScopeQuerier(t *testing.T) RowQuerier {
	sch := appdb.SchemaSQL{Schema: "t_toko_demo"}
	return func(_ context.Context, q string, _ ...any) Scanner {
		out := appdb.QualifySQL(sch, q)
		if strings.Contains(out, `""`) {
			t.Errorf("broken qualified SQL: %s", out)
			return scanFn(func(...any) error { return errors.New("syntax error") })
		}
		if strings.Contains(out, "business_price_type") {
			return scanFn(func(dest ...any) error {
				*dest[0].(*string), *dest[1].(*string) = "reseller", "Harga reseller"
				return nil
			})
		}
		return scanFn(func(...any) error { return sql.ErrNoRows })
	}
}

func TestLoadStatePricingResellerViaTenantScope(t *testing.T) {
	pricing, order := LoadState(context.Background(), tenantScopeQuerier(t), nil, "c1")
	if pricing == nil || pricing.Tier != "reseller" || pricing.TierLabel != "Harga reseller" {
		t.Fatalf("pricing = %+v, want reseller", pricing)
	}
	if order != nil {
		t.Fatalf("order = %+v, want nil (no rows)", order)
	}
}

func TestLoadStateReportsQueryErrors(t *testing.T) {
	var got []string
	old := OnLoadError
	OnLoadError = func(part, _ string, err error) { got = append(got, part+":"+err.Error()) }
	defer func() { OnLoadError = old }()
	failing := func(context.Context, string, ...any) Scanner {
		return scanFn(func(...any) error { return errors.New("pq: syntax error") })
	}
	if p, o := LoadState(context.Background(), failing, nil, "c1"); p != nil || o != nil {
		t.Fatal("expected nil views on error")
	}
	if len(got) != 2 || !strings.HasPrefix(got[0], "pricing:") || !strings.HasPrefix(got[1], "order:") {
		t.Fatalf("reported %v", got)
	}
	got = nil
	noRows := func(context.Context, string, ...any) Scanner {
		return scanFn(func(...any) error { return sql.ErrNoRows })
	}
	LoadState(context.Background(), noRows, nil, "c1")
	if len(got) != 0 {
		t.Fatalf("ErrNoRows must not be reported: %v", got)
	}
}

func TestDefaultQualifier(t *testing.T) {
	if DefaultQualifier("order") != `"order"` || DefaultQualifier("contact") != "contact" {
		t.Fatal("default qualifier")
	}
}
