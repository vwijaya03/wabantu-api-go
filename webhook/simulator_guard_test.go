package webhook

import (
	"context"
	"errors"
	"testing"
	"time"

	"encore.dev/beta/errs"
	"github.com/redis/go-redis/v9"

	sim "encore.app/wabantu/internal/simulator"
	"encore.app/wabantu/shared/types"
)

func TestSimulatorTenantGate(t *testing.T) {
	allow := sim.ParseAllowlist("t_toko_demo")
	if err := simulatorTenantGate(&types.AuthUser{TenantID: "demo-id", TenantSchema: "t_toko_demo"}, allow); err != nil {
		t.Fatalf("demo tenant rejected: %v", err)
	}
	for _, u := range []*types.AuthUser{
		{TenantID: "other-id", TenantSchema: "t_other"},
		nil,
	} {
		err := simulatorTenantGate(u, allow)
		var e *errs.Error
		if !errors.As(err, &e) || e.Code != errs.PermissionDenied || e.Code.HTTPStatus() != 403 {
			t.Fatalf("want 403 for %+v, got %v", u, err)
		}
	}
	if simulatorTenantGate(&types.AuthUser{TenantID: "demo-id", TenantSchema: "t_toko_demo"}, sim.ParseAllowlist("")) == nil {
		t.Fatal("empty allowlist must fail closed")
	}
}

// Redis unreachable (nil client or dead address): the in-memory limiter still
// enforces RatePerMinute per session, so the handler returns 429 after the limit.
func TestSimulatorRateLimitRedisDown(t *testing.T) {
	dead := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 50 * time.Millisecond, MaxRetries: -1})
	defer dead.Close()
	for name, rdb := range map[string]*redis.Client{"nil": nil, "dead": dead} {
		mem := sim.NewMemLimiter(nil)
		for i := 0; i < sim.RatePerMinute; i++ {
			if !simulatorRateOK(context.Background(), rdb, mem, "demo-id", "reseller-demo") {
				t.Fatalf("%s: hit %d rejected", name, i+1)
			}
		}
		if simulatorRateOK(context.Background(), rdb, mem, "demo-id", "reseller-demo") {
			t.Fatalf("%s: over-limit hit allowed with Redis down", name)
		}
		err := &errs.Error{Code: errs.ResourceExhausted}
		if err.Code.HTTPStatus() != 429 {
			t.Fatalf("ResourceExhausted must map to 429, got %d", err.Code.HTTPStatus())
		}
	}
}
