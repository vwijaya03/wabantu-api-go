package buyerflow

import (
	"strings"
	"testing"
)

func TestOmahBurst_stitchedTurnListsMaggiOnce(t *testing.T) {
	sim := &Simulator{
		Profile: foodProfile(),
		Catalog: omahLiveFMCGCatalog(),
	}
	stitched := "halo\nsaya mau beli\nmagi\nbisa ga ?"
	out := sim.Turn(stitched)
	if out.Path == PathGreeting {
		t.Fatalf("stitched burst must not greet, path=%s reply=%q", out.Path, out.Reply)
	}
	if out.Path != PathCatalogDB {
		t.Fatalf("path=%s want catalog_db reply=%q", out.Path, out.Reply)
	}
	lower := strings.ToLower(out.Reply)
	if strings.Count(lower, "maggi") < 2 || !strings.Contains(lower, "tandoori") {
		t.Fatalf("one catalog list of Maggi variants, got %q", out.Reply)
	}
}

func TestOmahBurst_channelRepairSilence(t *testing.T) {
	sim := &Simulator{
		Profile: foodProfile(),
		Catalog: omahLiveFMCGCatalog(),
	}
	out := sim.Turn("maaf chatnya putus putus")
	if out.Path != PathChannelMeta {
		t.Fatalf("path=%s want channel_meta reply=%q", out.Path, out.Reply)
	}
	if strings.TrimSpace(out.Reply) != "" {
		t.Fatalf("repair meta must not send a Maggi list, got %q", out.Reply)
	}
}
