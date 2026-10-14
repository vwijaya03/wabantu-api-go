package buyerflow

import (
	"strings"
	"testing"
)

func TestEnglishDraftOrderCancelReply(t *testing.T) {
	t.Parallel()
	assertFullDraftCancel := func(t *testing.T, msg string) {
		t.Helper()
		sim := newWB239488Sim()
		out := sim.Turn(msg)
		if out.Path != PathOrderCancel {
			t.Fatalf("%q path=%s want %s", msg, out.Path, PathOrderCancel)
		}
		if !out.Canceled || out.Order != nil {
			t.Fatalf("%q cart must be cleared: canceled=%v order=%v", msg, out.Canceled, out.Order)
		}
		low := strings.ToLower(out.Reply)
		if strings.Contains(low, "ordernya dibatalkan") || strings.Contains(low, "pesanan") {
			t.Fatalf("%q English cancel should not be Indonesian-only: %q", msg, out.Reply)
		}
		if !strings.Contains(low, "cancel") {
			t.Fatalf("%q expected English cancel ack: %q", msg, out.Reply)
		}
	}
	for _, msg := range []string{"cancel order", "cancel the order", "cancel my order"} {
		msg := msg
		t.Run(msg, func(t *testing.T) {
			t.Parallel()
			assertFullDraftCancel(t, msg)
		})
	}
	t.Run("cancel_the_maggi_line", func(t *testing.T) {
		t.Parallel()
		sim := newWB239488Sim()
		out := sim.Turn("cancel the maggi")
		if out.Canceled || out.Path == PathOrderCancel {
			t.Fatalf("line cancel must not void draft: path=%s canceled=%v", out.Path, out.Canceled)
		}
		if cartHasSKU(out.Order, "maggi-berempah") {
			t.Fatalf("Maggi line should be removed, cart=%+v", out.Order.Items)
		}
		if !cartHasSKU(out.Order, "cad-bar") || !cartHasSKU(out.Order, "durian-biscuit") {
			t.Fatalf("other lines must stay, cart=%+v", out.Order.Items)
		}
	})
}

func TestEnglishOrderFlowCancelReplyTemplate(t *testing.T) {
	reply := OrderFlowCancelReply("", "I want to cancel the order")
	if strings.Contains(reply, "ordernya") {
		t.Fatalf("expected English draft cancel template: %q", reply)
	}
}

func TestIndonesianDraftOrderCancelReply(t *testing.T) {
	sim := newWB239488Sim()
	out := sim.Turn("batalkan pesanan")
	if out.Path != PathOrderCancel {
		t.Fatalf("path=%s want %s", out.Path, PathOrderCancel)
	}
	if !strings.Contains(strings.ToLower(out.Reply), "batalkan") {
		t.Fatalf("Indonesian cancel regression: %q", out.Reply)
	}
}

func TestEnglishCasualPraiseReply(t *testing.T) {
	reply := CasualPraiseReply(false, "you are so helpful, thank you")
	if strings.Contains(reply, "Makasih") || strings.Contains(reply, "ditanyain") {
		t.Fatalf("expected English praise reply: %q", reply)
	}
	if !strings.Contains(strings.ToLower(reply), "thank") {
		t.Fatalf("expected English praise reply: %q", reply)
	}
}

func TestIndonesianCasualPraiseReply(t *testing.T) {
	reply := CasualPraiseReply(false, "mantap banget")
	if !strings.Contains(reply, "Makasih") {
		t.Fatalf("Indonesian praise regression: %q", reply)
	}
}

func TestEnglishAddMoreItemsPolicyReply(t *testing.T) {
	sim := newWB239488Sim()
	out := sim.Turn("can I add more items?")
	if out.Path != PathOrderFlow {
		t.Fatalf("path=%s want %s reply=%q", out.Path, PathOrderFlow, out.Reply)
	}
	if strings.Contains(out.Reply, "Siap kak") || strings.Contains(out.Reply, "nama produk") {
		t.Fatalf("expected English add-more policy: %q", out.Reply)
	}
	if !strings.Contains(strings.ToLower(out.Reply), "add more") {
		t.Fatalf("expected English add-more policy: %q", out.Reply)
	}
}

func TestIndonesianAddMoreItemsPolicyReply(t *testing.T) {
	sim := newWB239488Sim()
	out := sim.Turn("masih bisa tambah item lagi?")
	if out.Path != PathOrderFlow {
		t.Fatalf("path=%s want %s", out.Path, PathOrderFlow)
	}
	if !strings.Contains(out.Reply, "tambah item") {
		t.Fatalf("Indonesian add-more policy regression: %q", out.Reply)
	}
}

func TestEnglishPersistedOrderCancelCustomerReply(t *testing.T) {
	reply := OrderCancelCustomerReply("please cancel my order", "", "WB-ABC12345")
	if strings.Contains(reply, "kami batalkan") || strings.Contains(reply, "pesanan") {
		t.Fatalf("English buyer should not get Indonesian persisted cancel: %q", reply)
	}
	if !strings.Contains(strings.ToLower(reply), "cancel") {
		t.Fatalf("expected English persisted cancel: %q", reply)
	}
}
