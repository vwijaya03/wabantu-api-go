// DEMO-ONLY seed for the AI Builder Cup judging environment.
//
// Seeds, through the public HTTP API (so every tenant-schema side effect runs
// exactly like production code): a sample tenant "Toko Demo WABantu", three
// price tiers (umum / reseller / agen), tiered products, and a reseller buyer
// contact mapped to simulator session "reseller-demo".
//
// It NEVER touches a database directly and refuses to run unless explicitly
// confirmed and pointed at a separate demo deployment:
//
//	DEMO_SEED_CONFIRM=wabantu-demo-only \
//	DEMO_BASE_URL=https://wabantu-demo-xxxx.run.app \
//	DEMO_OWNER_EMAIL=demo-owner@example.com DEMO_OWNER_PASSWORD='...' \
//	go run ./scripts/cmd/demo-seed/
//
// Idempotent: re-running skips what already exists (matched by code/externalCode/phone).
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"encore.app/wabantu/internal/simulator"
)

const confirmValue = "wabantu-demo-only"

type tier struct{ Code, Label string }

// Tiers: umum + reseller are system-seeded per tenant; agen is created.
var tiers = []tier{
	{"umum", "Harga umum"},
	{"reseller", "Harga reseller"},
	{"agen", "Harga agen (DEMO)"},
}

type product struct {
	Code, Name, Unit, Desc string
	Prices                 map[string]float64 // tier code -> price
}

var products = []product{
	{"DEMO-KOPI-250", "Kopi Arabika Gayo 250g", "pcs", "Biji kopi sangrai medium, 250 gram.",
		map[string]float64{"umum": 85000, "reseller": 72000, "agen": 65000}},
	{"DEMO-KOPI-1K", "Kopi Arabika Gayo 1kg", "pcs", "Biji kopi sangrai medium, 1 kg.",
		map[string]float64{"umum": 310000, "reseller": 270000, "agen": 245000}},
	{"DEMO-TEH-100", "Teh Melati Tubruk 100g", "pcs", "Teh melati kering, 100 gram.",
		map[string]float64{"umum": 25000, "reseller": 20000, "agen": 17500}},
	{"DEMO-GULA-500", "Gula Aren Bubuk 500g", "pcs", "Gula aren organik bubuk.",
		map[string]float64{"umum": 45000, "reseller": 38000, "agen": 34000}},
	{"DEMO-CUP-50", "Paper Cup 8oz isi 50", "pak", "Gelas kertas 8oz, 1 pak isi 50.",
		map[string]float64{"umum": 30000, "reseller": 25000, "agen": 22000}},
}

// ResellerSession is the simulator sessionId whose buyer is priced as reseller.
const ResellerSession = "reseller-demo"

func checkGuard(getenv func(string) string) (string, error) {
	if getenv("DEMO_SEED_CONFIRM") != confirmValue {
		return "", fmt.Errorf("refusing: set DEMO_SEED_CONFIRM=%s (demo deployments only)", confirmValue)
	}
	raw := strings.TrimRight(getenv("DEMO_BASE_URL"), "/")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("DEMO_BASE_URL must be an absolute http(s) URL")
	}
	host := strings.ToLower(u.Hostname())
	if !strings.Contains(host, "demo") && host != "localhost" && host != "127.0.0.1" {
		return "", fmt.Errorf("refusing: host %q does not contain \"demo\" (use the separate demo Cloud Run service)", host)
	}
	if getenv("DEMO_OWNER_EMAIL") == "" || getenv("DEMO_OWNER_PASSWORD") == "" {
		return "", errors.New("DEMO_OWNER_EMAIL and DEMO_OWNER_PASSWORD are required")
	}
	return raw, nil
}

type client struct {
	base, token string
	hc          *http.Client
}

func (c *client) do(method, path string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("%s %s: %d %.200s", method, path, resp.StatusCode, b)
	}
	if out != nil && len(b) > 0 {
		return resp.StatusCode, json.Unmarshal(b, out)
	}
	return resp.StatusCode, nil
}

type authResp struct {
	AccessToken string `json:"accessToken"`
	User        struct {
		Tenant *struct {
			ID string `json:"id"`
		} `json:"tenant"`
	} `json:"user"`
}

type priceType struct {
	ID   string `json:"id"`
	Code string `json:"code"`
}

type itemPrice struct {
	PriceTypeID string  `json:"priceTypeId"`
	Price       float64 `json:"price"`
}

func buildCatalogRequest(p product, ids map[string]string) (map[string]any, error) {
	prices := []itemPrice{}
	for _, t := range tiers {
		v, ok := p.Prices[t.Code]
		if !ok {
			continue
		}
		id := ids[t.Code]
		if id == "" {
			return nil, fmt.Errorf("price type %q missing", t.Code)
		}
		prices = append(prices, itemPrice{id, v})
	}
	sell := p.Prices["umum"]
	active := true
	return map[string]any{
		"externalCode": p.Code, "name": p.Name, "description": p.Desc,
		"sellPrice": sell, "sellUnit": p.Unit, "isActive": active, "prices": prices,
	}, nil
}

func run() error {
	base, err := checkGuard(os.Getenv)
	if err != nil {
		return err
	}
	c := &client{base: base, hc: &http.Client{Timeout: 30 * time.Second}}
	email, pw := os.Getenv("DEMO_OWNER_EMAIL"), os.Getenv("DEMO_OWNER_PASSWORD")

	var ar authResp
	st, err := c.do("POST", "/api/v1/auth/register", map[string]string{
		"email": email, "password": pw, "name": "Owner Demo", "businessName": "Toko Demo WABantu", "slug": "toko-demo",
	}, &ar)
	if st == http.StatusConflict {
		fmt.Println("tenant: already registered, logging in")
		ar = authResp{}
		_, err = c.do("POST", "/api/v1/auth/login", map[string]string{"email": email, "password": pw}, &ar)
	}
	if err != nil {
		return err
	}
	if ar.AccessToken == "" || ar.User.Tenant == nil {
		return errors.New("no token/tenant in auth response")
	}
	c.token = ar.AccessToken
	tenantID := ar.User.Tenant.ID
	fmt.Println("tenant:", tenantID)

	var pts struct{ Items []priceType }
	if _, err := c.do("GET", "/api/v1/business/price-types?pageSize=100", nil, &pts); err != nil {
		return err
	}
	ids := map[string]string{}
	for _, p := range pts.Items {
		ids[p.Code] = p.ID
	}
	for i, t := range tiers {
		if ids[t.Code] != "" {
			continue
		}
		var pt priceType
		if _, err := c.do("POST", "/api/v1/business/price-types", map[string]any{"code": t.Code, "label": t.Label, "displayOrder": i + 1}, &pt); err != nil {
			return err
		}
		ids[t.Code] = pt.ID
		fmt.Println("price type created:", t.Code)
	}

	for _, p := range products {
		var list struct {
			Items []struct{ ExternalCode string } `json:"items"`
		}
		if _, err := c.do("GET", "/api/v1/business/catalog?pageSize=100&q="+url.QueryEscape(p.Code), nil, &list); err != nil {
			return err
		}
		exists := false
		for _, it := range list.Items {
			exists = exists || it.ExternalCode == p.Code
		}
		if exists {
			fmt.Println("product exists:", p.Code)
			continue
		}
		req, err := buildCatalogRequest(p, ids)
		if err != nil {
			return err
		}
		if _, err := c.do("POST", "/api/v1/business/catalog", req, nil); err != nil {
			return err
		}
		fmt.Println("product created:", p.Code)
	}

	phone := simulator.BuyerPhone(tenantID, ResellerSession)
	name, rid := "Reseller Demo (simulator)", ids["reseller"]
	st, err = c.do("POST", "/api/v1/inbox/contacts", map[string]any{
		"phoneNumber": phone, "displayName": name, "priceTypeId": rid, "tags": []string{"demo"},
	}, nil)
	switch {
	case err == nil:
		fmt.Println("reseller contact created:", phone)
	case st == http.StatusConflict || st == http.StatusAlreadyReported:
		fmt.Println("reseller contact exists:", phone)
	default:
		return err
	}
	fmt.Printf("done. Simulator: sessionId=%q buys at reseller price; any other sessionId buys at umum.\n", ResellerSession)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "demo-seed:", err)
		os.Exit(1)
	}
}
