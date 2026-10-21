package salesbrain

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 17, 14, 20, 0, 0, time.UTC)

func snap(ref, status string, age time.Duration) OrderSnapshot {
	return OrderSnapshot{Ref: ref, Status: status, Stage: StageAddress, UpdatedAt: t0.Add(-age),
		Lines: []OrderLine{{Name: "Salmon Steak 100 Gram", Qty: 1, UnitPrice: 75000}}, Subtotal: 75000}
}

func TestSelectRemindOrders(t *testing.T) {
	got := SelectRemindOrders([]OrderSnapshot{
		snap("WB-OLD", StatusDraft, 73*time.Hour),
		snap("WB-PAID", "paid", time.Hour),
		snap("WB-SHIP", "shipped", time.Hour),
		snap("WB-A", StatusDraft, 3*time.Hour),
		snap("WB-B", StatusAwaitingPayment, time.Hour),
		snap("WB-C", "cancelled", time.Minute),
	}, t0)
	if len(got) != 2 || got[0].Ref != "WB-B" || got[1].Ref != "WB-A" {
		t.Fatalf("got %+v, want [WB-B WB-A] newest first", got)
	}
}

func TestSelectRemindOrdersCapsAtThree(t *testing.T) {
	var in []OrderSnapshot
	for i := 0; i < 5; i++ {
		in = append(in, snap("WB-"+string(rune('A'+i)), StatusDraft, time.Duration(i+1)*time.Hour))
	}
	if got := SelectRemindOrders(in, t0); len(got) != 3 || got[0].Ref != "WB-A" {
		t.Fatalf("got %d orders (first %q), want 3 newest first", len(got), got[0].Ref)
	}
}

func TestReminderAllowed(t *testing.T) {
	open := []OrderSnapshot{snap("WB-F4BFCE06", StatusDraft, time.Hour)}
	newSession := ReminderContext{Now: t0, PrevBuyerMessageAt: t0.Add(-45 * time.Minute)}

	if !ReminderAllowed(open, ReminderState{}, newSession) {
		t.Fatal("new session with an open order must allow a reminder")
	}
	if ReminderAllowed(nil, ReminderState{}, newSession) {
		t.Fatal("no open order → no reminder")
	}
	sameSession := ReminderContext{Now: t0, PrevBuyerMessageAt: t0.Add(-5 * time.Minute)}
	if ReminderAllowed(open, ReminderState{}, sameSession) {
		t.Fatal("buyer still mid-conversation (<30 min) → no reminder")
	}
	remindedThisSession := ReminderState{LastAt: t0.Add(-40 * time.Minute).Add(time.Minute)}
	ctx := ReminderContext{Now: t0, PrevBuyerMessageAt: t0.Add(-40 * time.Minute)}
	if ReminderAllowed(open, remindedThisSession, ctx) {
		t.Fatal("already reminded after the previous buyer message → no second reminder")
	}
	twice := ReminderState{LastAt: t0.Add(-10 * time.Hour), Count: map[string]int{"WB-F4BFCE06": 2}}
	if ReminderAllowed(open, twice, newSession) {
		t.Fatal("max 2 proactive reminders per order")
	}
	for _, rc := range []ReminderContext{
		{Now: t0, PrevBuyerMessageAt: t0.Add(-time.Hour), FillingField: true},
		{Now: t0, PrevBuyerMessageAt: t0.Add(-time.Hour), Complaint: true},
		{Now: t0, PrevBuyerMessageAt: t0.Add(-time.Hour), HandoffActive: true},
	} {
		if ReminderAllowed(open, ReminderState{}, rc) {
			t.Fatalf("no reminder while filling a field / complaining / in handoff: %+v", rc)
		}
	}
	firstEver := ReminderContext{Now: t0}
	if !ReminderAllowed(open, ReminderState{}, firstEver) {
		t.Fatal("no previous buyer message counts as a new session")
	}
}

func TestDescribeOrder(t *testing.T) {
	o := OrderSnapshot{Ref: "WB-F4BFCE06", Status: StatusDraft, Stage: StageAddress, Subtotal: 425000,
		Lines: []OrderLine{
			{Name: "Salmon Steak 100 Gram", Qty: 1, UnitPrice: 75000},
			{Name: "Yellow Tail Tuna Portion 100 Gram", Qty: 1, UnitPrice: 350000},
		}}
	en := DescribeOrder(o, LangEN)
	wantEN := "WB-F4BFCE06: 1x Salmon Steak 100 Gram + 1x Yellow Tail Tuna Portion 100 Gram, total Rp425.000 (shipping not included), just waiting for the delivery address"
	if en != wantEN {
		t.Errorf("EN:\n got %q\nwant %q", en, wantEN)
	}
	id := DescribeOrder(o, LangID)
	wantID := "WB-F4BFCE06: 1x Salmon Steak 100 Gram + 1x Yellow Tail Tuna Portion 100 Gram, total Rp425.000 (belum termasuk ongkir), tinggal alamat pengiriman"
	if id != wantID {
		t.Errorf("ID:\n got %q\nwant %q", id, wantID)
	}
	many := o
	many.Lines = append(many.Lines, OrderLine{Name: "A", Qty: 1}, OrderLine{Name: "B", Qty: 2})
	if got := DescribeOrder(many, LangEN); !strings.Contains(got, "+1 more items") {
		t.Errorf("more than 3 lines must be summarised, got %q", got)
	}
}

func TestFormatRupiah(t *testing.T) {
	for in, want := range map[int64]string{0: "Rp0", 75000: "Rp75.000", 425000: "Rp425.000", 1750000: "Rp1.750.000", 999: "Rp999"} {
		if got := FormatRupiah(in); got != want {
			t.Errorf("FormatRupiah(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestListUnfinishedOrdersHasNoAgeLimitAndCapsAtTen(t *testing.T) {
	old := OrderSnapshot{Ref: "WB-OLD", Status: StatusDraft, UpdatedAt: t0.Add(-30 * 24 * time.Hour)}
	list, more := ListUnfinishedOrders([]OrderSnapshot{old})
	if len(list) != 1 || more != 0 {
		t.Fatalf("a 30-day-old unfinished order is still listed: %+v more=%d", list, more)
	}
	var many []OrderSnapshot
	for i := 0; i < 13; i++ {
		many = append(many, OrderSnapshot{Ref: fmt.Sprintf("WB-%02d", i), UpdatedAt: t0.Add(-time.Duration(i) * time.Hour)})
	}
	list, more = ListUnfinishedOrders(many)
	if len(list) != MaxUnfinishedListed || more != 3 || list[0].Ref != "WB-00" {
		t.Fatalf("cap/newest-first wrong: len=%d more=%d first=%s", len(list), more, list[0].Ref)
	}
	if got := SelectRemindOrders([]OrderSnapshot{old}, t0); len(got) != 0 {
		t.Fatalf("the reminder keeps its 72-hour rule: %+v", got)
	}
}

func TestBuildFactsAllowsAmountsOfEveryListedOrder(t *testing.T) {
	f := BuildFacts(FactsInput{OpenOrders: []OrderSnapshot{
		{Ref: "WB-12345678", Status: StatusProcessing, Stage: StageProcessing, Paid: true, Subtotal: 777000,
			Lines: []OrderLine{{Name: "Tuna", Qty: 3, UnitPrice: 259000}}},
	}})
	if !f.Amounts[777000] || !f.Amounts[259000] {
		t.Fatalf("amounts of a listed (paid) order must be groundable: %+v", f.Amounts)
	}
}
