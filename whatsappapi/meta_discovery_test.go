package whatsappapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"encore.app/wabantu/whatsapp"
)

func TestPickWabaPhoneMatchesTarget(t *testing.T) {
	wabas := []metaWabaNode{{
		ID: "waba1",
		PhoneNumbers: struct {
			Data []metaPhoneNode `json:"data"`
		}{Data: []metaPhoneNode{
			{ID: "pn1", DisplayPhoneNumber: "+62 857-3086-4277"},
			{ID: "pn2", DisplayPhoneNumber: "+1 555 0001"},
		}},
	}}
	got := pickWabaPhone(wabas, "+6285730864277")
	if got.PhoneNumberID != "pn1" || got.WabaID != "waba1" {
		t.Fatalf("pickWabaPhone() = %+v want pn1/waba1", got)
	}
}

func TestFetchMetaWabaFromBusinessesGraphChain(t *testing.T) {
	const (
		bizID   = "biz123"
		wabaID  = "waba456"
		phoneID = "1028865826986337"
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := "/" + whatsapp.GraphAPIVersion
		switch r.URL.Path {
		case prefix + "/me/businesses":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": bizID, "name": "Test Biz"}},
			})
		case prefix + "/" + bizID + "/owned_whatsapp_business_accounts":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": wabaID, "name": "WABA"}},
			})
		case prefix + "/" + wabaID + "/phone_numbers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{
					"id":                   phoneID,
					"display_phone_number": "+6285730864277",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldBase := metaGraphBaseURL
	metaGraphBaseURL = srv.URL
	defer func() { metaGraphBaseURL = oldBase }()

	got := fetchMetaWabaFromBusinesses(context.Background(), "token", "+6285730864277")
	if got.PhoneNumberID != phoneID || got.WabaID != wabaID {
		t.Fatalf("fetchMetaWabaFromBusinesses() = %+v want phone=%s waba=%s", got, phoneID, wabaID)
	}
}

func TestFetchMetaWabaSkipsLegacyWhenBusinessesWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := "/" + whatsapp.GraphAPIVersion
		if r.URL.Path == prefix+"/me/businesses" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "biz1"}},
			})
			return
		}
		if r.URL.Path == prefix+"/biz1/owned_whatsapp_business_accounts" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "w1"}},
			})
			return
		}
		if r.URL.Path == prefix+"/w1/phone_numbers" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "p1", "display_phone_number": "62811"}},
			})
			return
		}
		if r.URL.Path == prefix+"/me" {
			t.Fatal("legacy /me should not be called when businesses flow succeeds")
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	oldBase := metaGraphBaseURL
	metaGraphBaseURL = srv.URL
	defer func() { metaGraphBaseURL = oldBase }()

	got := fetchMetaWaba(context.Background(), "token", "62811", "", "")
	if got.PhoneNumberID != "p1" {
		t.Fatalf("fetchMetaWaba() = %+v", got)
	}
}
