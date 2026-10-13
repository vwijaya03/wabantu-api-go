package simulator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"encore.app/wabantu/internal/realtime"
)

type fakeRow struct {
	vals []any
	err  error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		switch p := d.(type) {
		case *bool:
			*p = r.vals[i].(bool)
		case *string:
			*p = r.vals[i].(string)
		case *float64:
			*p = r.vals[i].(float64)
		case *[]byte:
			*p = []byte(r.vals[i].(string))
		case *time.Time:
			*p = r.vals[i].(time.Time)
		}
	}
	return nil
}

type fw struct {
	mu   sync.Mutex
	docs map[string]map[string]any
}

func (f *fw) Set(_ context.Context, p string, d map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.docs[p] = d
	return nil
}

func fakeDB(isSim bool) RowQuerier {
	return func(_ context.Context, q string, _ ...any) Scanner {
		switch {
		case strings.Contains(q, "provider = 'simulator'"):
			return fakeRow{vals: []any{isSim}}
		case strings.Contains(q, "business_price_type"):
			return fakeRow{vals: []any{"reseller", "Harga reseller"}}
		case strings.Contains(q, `"order"`):
			return fakeRow{vals: []any{"o1", "confirmed", "proof_submitted", 144000.0,
				`[{"name":"Kopi","qty":2,"unitPrice":72000}]`, `{"amount":144000}`, time.Now()}}
		}
		return fakeRow{err: errors.New("unexpected")}
	}
}

func TestMirrorSimulatorConversation(t *testing.T) {
	f := &fw{docs: map[string]map[string]any{}}
	m := &realtime.Mirror{W: f}
	Mirror(context.Background(), m, fakeDB(true), nil, "t1", "c1",
		&MirrorMsg{ID: "m1", Direction: "in", Type: "image", CreatedAt: time.Now()})
	msg := f.docs["tenants/t1/simConversations/c1/messages/m1"]
	if msg == nil || msg["direction"] != "inbound" || msg["imageUrl"] != "/api/v1/inbox/messages/m1/media" {
		t.Fatalf("%v", f.docs)
	}
	conv := f.docs["tenants/t1/simConversations/c1"]
	ov, _ := conv["order"].(*OrderView)
	pv, _ := conv["pricing"].(*PricingView)
	if ov == nil || ov.ProofCheck != "cocok" || pv == nil || pv.Tier != "reseller" {
		t.Fatalf("%v", conv)
	}
	if f.docs["tenants/t1/orders/o1"] == nil {
		t.Fatal("order doc missing")
	}
}

func TestMirrorSkipsNonSimulatorAndDisabled(t *testing.T) {
	f := &fw{docs: map[string]map[string]any{}}
	Mirror(context.Background(), &realtime.Mirror{W: f}, fakeDB(false), nil, "t1", "c1", &MirrorMsg{ID: "m1"})
	Mirror(context.Background(), &realtime.Mirror{}, fakeDB(true), nil, "t1", "c1", &MirrorMsg{ID: "m1"})
	Mirror(context.Background(), nil, fakeDB(true), nil, "t1", "c1", nil)
	if len(f.docs) != 0 {
		t.Fatalf("%v", f.docs)
	}
}

func TestMirrorBroadcastUsesPayloadTenant(t *testing.T) {
	f := &fw{docs: map[string]map[string]any{}}
	m := &realtime.Mirror{W: f}
	if !MirrorBroadcast(context.Background(), m, fakeDB(true), SchemaQualifier("t_abc"), "t1", "c1", "bm1", "Promo!", time.Now()) {
		t.Fatal("expected mirror")
	}
	d := f.docs["tenants/t1/simConversations/c1/messages/bm1"]
	if d == nil || d["text"] != "Promo!" || d["author"] != "human" || d["direction"] != "outbound" {
		t.Fatalf("%v", f.docs)
	}
	// Legacy job without tenantId: delivered elsewhere, mirror skipped (no write, no panic).
	f2 := &fw{docs: map[string]map[string]any{}}
	if MirrorBroadcast(context.Background(), &realtime.Mirror{W: f2}, fakeDB(true), nil, "", "c1", "bm1", "x", time.Now()) || len(f2.docs) != 0 {
		t.Fatal("must skip without tenantId")
	}
}

func TestSchemaQualifier(t *testing.T) {
	if SchemaQualifier("t_abc")("order") != `"t_abc"."order"` {
		t.Fatal(SchemaQualifier("t_abc")("order"))
	}
}
