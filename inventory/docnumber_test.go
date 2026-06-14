package inventory

import "testing"

func TestFormatDocNumber(t *testing.T) {
	cases := map[string]struct {
		prefix string
		n      int64
	}{
		"WPO-000001":  {"WPO", 1},
		"WPO-000042":  {"WPO", 42},
		"WBIL-000018": {"WBIL", 18},
		"WINV-000201": {"WINV", 201},
		"WRET-123456": {"WRET", 123456},
		"WPO-1000000": {"WPO", 1000000}, // overflow pad: still renders full number
	}
	for want, in := range cases {
		if got := formatDocNumber(in.prefix, in.n); got != want {
			t.Fatalf("formatDocNumber(%q,%d) = %q, want %q", in.prefix, in.n, got, want)
		}
	}
}

func TestPOStatusFromReceipts(t *testing.T) {
	cases := []struct {
		ordered, received float64
		want              string
	}{
		{10, 0, "open"},
		{10, 4, "partial"},
		{10, 9.9999, "partial"},
		{10, 10, "received"},
		{10, 10.5, "received"}, // over-receipt still counts as received
		{1.5, 1.5, "received"}, // fractional
		{2, 1, "partial"},
	}
	for _, c := range cases {
		if got := poStatusFromReceipts(c.ordered, c.received); got != c.want {
			t.Fatalf("poStatusFromReceipts(%v,%v) = %q, want %q", c.ordered, c.received, got, c.want)
		}
	}
}
