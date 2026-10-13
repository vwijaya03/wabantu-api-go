package main

import "testing"

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestGuard(t *testing.T) {
	ok := map[string]string{"DEMO_SEED_CONFIRM": confirmValue, "DEMO_BASE_URL": "https://wabantu-demo-abc.run.app/", "DEMO_OWNER_EMAIL": "a@b.c", "DEMO_OWNER_PASSWORD": "x"}
	if b, err := checkGuard(env(ok)); err != nil || b != "https://wabantu-demo-abc.run.app" {
		t.Fatal(b, err)
	}
	for _, mut := range []func(map[string]string){
		func(m map[string]string) { m["DEMO_SEED_CONFIRM"] = "yes" },
		func(m map[string]string) { m["DEMO_BASE_URL"] = "https://api.wabantu.com" },
		func(m map[string]string) { m["DEMO_BASE_URL"] = "notaurl" },
		func(m map[string]string) { m["DEMO_OWNER_PASSWORD"] = "" },
	} {
		m := map[string]string{}
		for k, v := range ok {
			m[k] = v
		}
		mut(m)
		if _, err := checkGuard(env(m)); err == nil {
			t.Fatalf("expected refusal for %v", m)
		}
	}
}

func TestProductsTiered(t *testing.T) {
	ids := map[string]string{"umum": "1", "reseller": "2", "agen": "3"}
	for _, p := range products {
		if !(p.Prices["umum"] > p.Prices["reseller"] && p.Prices["reseller"] > p.Prices["agen"]) {
			t.Fatalf("%s tiers not descending", p.Code)
		}
		r, err := buildCatalogRequest(p, ids)
		if err != nil || len(r["prices"].([]itemPrice)) != 3 || r["sellPrice"] != p.Prices["umum"] {
			t.Fatal(p.Code, err)
		}
	}
	if _, err := buildCatalogRequest(products[0], map[string]string{"umum": "1"}); err == nil {
		t.Fatal("missing tier must error")
	}
}
