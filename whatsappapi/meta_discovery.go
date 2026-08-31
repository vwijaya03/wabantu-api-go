package whatsappapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"encore.dev/rlog"

	"encore.app/wabantu/whatsapp"
)

// metaGraphBaseURL is overrideable in tests (httptest server).
var metaGraphBaseURL = "https://graph.facebook.com"

type wabaDiscovery struct {
	WabaID        string
	PhoneNumberID string
}

type metaPhoneNode struct {
	ID                 string `json:"id"`
	DisplayPhoneNumber string `json:"display_phone_number"`
}

type metaWabaNode struct {
	ID           string `json:"id"`
	PhoneNumbers struct {
		Data []metaPhoneNode `json:"data"`
	} `json:"phone_numbers"`
}

func truncGraphBody(body []byte) string {
	const max = 400
	s := strings.TrimSpace(string(body))
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func fetchMetaWaba(ctx context.Context, accessToken, targetPhone, appID, appSecret string) wabaDiscovery {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return wabaDiscovery{}
	}
	if d := fetchMetaWabaFromBusinesses(ctx, accessToken, targetPhone); d.PhoneNumberID != "" {
		return d
	}
	if d := fetchMetaWabaFromDebugToken(ctx, accessToken, appID, appSecret, targetPhone); d.PhoneNumberID != "" {
		return d
	}
	if d := fetchMetaWabaLegacyMe(ctx, accessToken, targetPhone); d.PhoneNumberID != "" {
		return d
	}
	return wabaDiscovery{}
}

// fetchMetaWabaFromDebugToken reads WABA IDs from token granular_scopes (Embedded Signup / OAuth).
func fetchMetaWabaFromDebugToken(ctx context.Context, userToken, appID, appSecret, targetPhone string) wabaDiscovery {
	appID = strings.TrimSpace(appID)
	appSecret = strings.TrimSpace(appSecret)
	userToken = strings.TrimSpace(userToken)
	if appID == "" || appSecret == "" || userToken == "" {
		return wabaDiscovery{}
	}
	appToken := appID + "|" + appSecret
	body, status, err := metaGraphGET(ctx, appToken, "/debug_token", "input_token", userToken)
	if err != nil || status < 200 || status >= 300 {
		if status >= 400 {
			rlog.Warn("fetchMetaWaba debug_token non-2xx", "status", status, "body", truncGraphBody(body))
		}
		return wabaDiscovery{}
	}
	var parsed struct {
		Data struct {
			GranularScopes []struct {
				Scope     string   `json:"scope"`
				TargetIDs []string `json:"target_ids"`
			} `json:"granular_scopes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return wabaDiscovery{}
	}
	var wabaIDs []string
	for _, gs := range parsed.Data.GranularScopes {
		if gs.Scope != "whatsapp_business_management" && gs.Scope != "whatsapp_business_messaging" {
			continue
		}
		for _, id := range gs.TargetIDs {
			id = strings.TrimSpace(id)
			if id != "" {
				wabaIDs = append(wabaIDs, id)
			}
		}
	}
	if len(wabaIDs) == 0 {
		return wabaDiscovery{}
	}
	return discoverPhonesFromWABAs(ctx, userToken, wabaIDs, targetPhone)
}

func discoverPhonesFromWABAs(ctx context.Context, accessToken string, wabaIDs []string, targetPhone string) wabaDiscovery {
	var best, fallback wabaDiscovery
	hasFallback := false
	for _, wabaID := range wabaIDs {
		phonesBody, phonesStatus, err := metaGraphGET(ctx, accessToken,
			"/"+wabaID+"/phone_numbers", "fields", "id,display_phone_number")
		if err != nil || phonesStatus < 200 || phonesStatus >= 300 {
			continue
		}
		var phones struct {
			Data []metaPhoneNode `json:"data"`
		}
		if err := json.Unmarshal(phonesBody, &phones); err != nil || len(phones.Data) == 0 {
			continue
		}
		if !hasFallback {
			fallback = wabaDiscovery{WabaID: wabaID, PhoneNumberID: phones.Data[0].ID}
			hasFallback = true
		}
		for _, phone := range phones.Data {
			if phoneMatchesTarget(phone.DisplayPhoneNumber, targetPhone) && phone.ID != "" {
				return wabaDiscovery{WabaID: wabaID, PhoneNumberID: phone.ID}
			}
		}
		if best.PhoneNumberID == "" {
			best = wabaDiscovery{WabaID: wabaID, PhoneNumberID: phones.Data[0].ID}
		}
	}
	if best.PhoneNumberID != "" {
		return best
	}
	return fallback
}

// fetchMetaWabaFromBusinesses uses Graph API v25+ flow:
// GET /me/businesses → GET /{business-id}/owned_whatsapp_business_accounts → GET /{waba-id}/phone_numbers
func fetchMetaWabaFromBusinesses(ctx context.Context, accessToken, targetPhone string) wabaDiscovery {
	body, status, err := metaGraphGET(ctx, accessToken, "/me/businesses", "fields", "id,name")
	if err != nil {
		rlog.Warn("fetchMetaWaba businesses request failed", "err", err)
		return wabaDiscovery{}
	}
	if status < 200 || status >= 300 {
		rlog.Warn("fetchMetaWaba /me/businesses non-2xx", "status", status, "body", truncGraphBody(body))
		return wabaDiscovery{}
	}

	var businesses struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &businesses); err != nil {
		rlog.Warn("fetchMetaWaba parse businesses failed", "err", err, "body", truncGraphBody(body))
		return wabaDiscovery{}
	}

	var wabaIDs []string
	for _, biz := range businesses.Data {
		if strings.TrimSpace(biz.ID) == "" {
			continue
		}
		wabaBody, wabaStatus, err := metaGraphGET(ctx, accessToken,
			"/"+biz.ID+"/owned_whatsapp_business_accounts", "fields", "id,name")
		if err != nil || wabaStatus < 200 || wabaStatus >= 300 {
			continue
		}
		var wabas struct {
			Data []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"data"`
		}
		if err := json.Unmarshal(wabaBody, &wabas); err != nil {
			continue
		}
		for _, waba := range wabas.Data {
			if id := strings.TrimSpace(waba.ID); id != "" {
				wabaIDs = append(wabaIDs, id)
			}
		}
	}
	if len(wabaIDs) == 0 {
		rlog.Warn("fetchMetaWaba no WABA from /me/businesses", "body", truncGraphBody(body))
		return wabaDiscovery{}
	}
	return discoverPhonesFromWABAs(ctx, accessToken, wabaIDs, targetPhone)
}

// fetchMetaWabaLegacyMe supports older Graph responses on /me (pre-v25).
func fetchMetaWabaLegacyMe(ctx context.Context, accessToken, targetPhone string) wabaDiscovery {
	body, status, err := metaGraphGET(ctx, accessToken, "/me", "fields",
		"whatsapp_business_accounts{id,phone_numbers{id,display_phone_number}}")
	if err != nil {
		rlog.Warn("fetchMetaWaba legacy /me request failed", "err", err)
		return wabaDiscovery{}
	}
	if status < 200 || status >= 300 {
		rlog.Warn("fetchMetaWaba legacy /me non-2xx", "status", status, "body", truncGraphBody(body))
		return wabaDiscovery{}
	}

	var parsed struct {
		WhatsAppBusinessAccounts json.RawMessage `json:"whatsapp_business_accounts"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		rlog.Warn("fetchMetaWaba legacy parse failed", "err", err, "body", truncGraphBody(body))
		return wabaDiscovery{}
	}

	wabas, ok := parseWabaNodes(parsed.WhatsAppBusinessAccounts)
	if !ok || len(wabas) == 0 {
		return wabaDiscovery{}
	}
	return pickWabaPhone(wabas, targetPhone)
}

func metaGraphGET(ctx context.Context, accessToken, path string, queryKeyVals ...string) ([]byte, int, error) {
	base, err := url.Parse(metaGraphBaseURL)
	if err != nil {
		return nil, 0, err
	}
	u := url.URL{
		Scheme: base.Scheme,
		Host:   base.Host,
		Path:   "/" + whatsapp.GraphAPIVersion + path,
	}
	q := u.Query()
	for i := 0; i+1 < len(queryKeyVals); i += 2 {
		q.Set(queryKeyVals[i], queryKeyVals[i+1])
	}
	q.Set("access_token", accessToken)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func parseWabaNodes(raw json.RawMessage) ([]metaWabaNode, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	var wabas []metaWabaNode
	if err := json.Unmarshal(raw, &wabas); err == nil {
		return wabas, true
	}
	var wrapped struct {
		Data []metaWabaNode `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, false
	}
	return wrapped.Data, true
}

func pickWabaPhone(wabas []metaWabaNode, targetPhone string) wabaDiscovery {
	target := normalizePhone(targetPhone)
	var fallbackPhoneID string
	var fallbackWabaID string
	for _, waba := range wabas {
		phones := waba.PhoneNumbers.Data
		if len(phones) == 0 {
			continue
		}
		if fallbackPhoneID == "" && phones[0].ID != "" {
			fallbackPhoneID = phones[0].ID
			fallbackWabaID = waba.ID
		}
		for _, phone := range phones {
			if phoneMatchesTarget(phone.DisplayPhoneNumber, target) && phone.ID != "" {
				return wabaDiscovery{WabaID: waba.ID, PhoneNumberID: phone.ID}
			}
		}
	}
	if fallbackPhoneID != "" {
		return wabaDiscovery{WabaID: fallbackWabaID, PhoneNumberID: fallbackPhoneID}
	}
	return wabaDiscovery{}
}

func phoneMatchesTarget(displayPhone, target string) bool {
	candidate := normalizePhone(displayPhone)
	if candidate == "" || target == "" {
		return false
	}
	return candidate == target ||
		strings.HasSuffix(candidate, target) ||
		strings.HasSuffix(target, candidate)
}
