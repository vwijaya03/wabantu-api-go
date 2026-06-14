package inventory

import "testing"

func TestNormalizeCostingMethod(t *testing.T) {
	valid := map[string]string{
		"fifo":      "fifo",
		"FIFO":      "fifo",
		"  lifo  ":  "lifo",
		"Average":   "average",
		"AVERAGE":   "average",
	}
	for in, want := range valid {
		got, ok := normalizeCostingMethod(in)
		if !ok || got != want {
			t.Fatalf("normalizeCostingMethod(%q) = (%q,%v), want (%q,true)", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "   ", "fff", "weighted", "fi fo", "moving-average"} {
		if got, ok := normalizeCostingMethod(in); ok {
			t.Fatalf("normalizeCostingMethod(%q) = (%q,true), want invalid", in, got)
		}
	}
}

func TestEffectiveCostingMethod(t *testing.T) {
	cases := []struct {
		override, def, want string
	}{
		{"fifo", "average", "fifo"},   // SKU override wins
		{"", "lifo", "lifo"},          // fall back to tenant default
		{"  ", "fifo", "fifo"},        // blank override
		{"garbage", "lifo", "lifo"},   // invalid override -> default
		{"", "", "average"},           // nothing set -> safe default
		{"bad", "alsoBad", "average"}, // both invalid -> safe default
		{"LIFO", "fifo", "lifo"},      // case-insensitive override
	}
	for _, c := range cases {
		if got := effectiveCostingMethod(c.override, c.def); got != c.want {
			t.Fatalf("effectiveCostingMethod(%q,%q) = %q, want %q", c.override, c.def, got, c.want)
		}
	}
}

func TestNormalizeWarehouseCode(t *testing.T) {
	cases := map[string]string{
		"Gudang Utama":      "gudang-utama",
		"  Gudang  Pusat ":  "gudang-pusat",
		"Gudang #1 (Jkt)":   "gudang-1-jkt",
		"---":               "gudang",
		"":                  "gudang",
		"GUDANG":            "gudang",
		"Toko@Bekasi!!":     "toko-bekasi",
	}
	for in, want := range cases {
		if got := normalizeWarehouseCode(in); got != want {
			t.Fatalf("normalizeWarehouseCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeWarehouseCodeMaxLen(t *testing.T) {
	long := ""
	for i := 0; i < 60; i++ {
		long += "a"
	}
	got := normalizeWarehouseCode(long)
	if len(got) > 40 {
		t.Fatalf("code length = %d, want <= 40", len(got))
	}
}
