package ai

import (
	"strings"
	"testing"
)

func TestFormatOrderNumber(t *testing.T) {
	got := FormatOrderNumber("eb76635c-8439-42f1-9a45-dfa31bc0bbf4")
	want := "WB-EB76635C"
	if got != want {
		t.Fatalf("FormatOrderNumber = %q, want %q", got, want)
	}
	if FormatOrderNumber("") != "" {
		t.Fatal("empty id should return empty ref")
	}
}

func TestIsOrderCancelRequest(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"mau saya batalkan ya", true},
		{"batalkan pesanan", true},
		{"tidak jadi order", true},
		{"pesanan yang atas nama saya ada kah ?", false},
		{"harga berapa", false},
	}
	for _, tc := range cases {
		if got := IsOrderCancelRequest(tc.text); got != tc.want {
			t.Fatalf("IsOrderCancelRequest(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestIsOrderStatusInquiry(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"pesanan yang atas nama saya ada kah ?", true},
		{"status pesanan saya", true},
		{"mau saya batalkan ya", false},
		{"halo kak", false},
	}
	for _, tc := range cases {
		if got := IsOrderStatusInquiry(tc.text); got != tc.want {
			t.Fatalf("IsOrderStatusInquiry(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestFormatPersistedOrderSummary(t *testing.T) {
	o := &persistedOrder{
		ID:     "eb76635c-8439-42f1-9a45-dfa31bc0bbf4",
		Status: "draft",
		ItemsJSON: []byte(`[{"name":"Celana Dalam Boxer","qty":5,"unitPrice":21500}]`),
		ShippingJSON: []byte(`{"name":"Antoni","city":"Magelang"}`),
		Total: 107500,
	}
	summary := formatPersistedOrderSummary(o)
	if !strings.Contains(summary, "WB-EB76635C") {
		t.Fatalf("summary missing order ref: %s", summary)
	}
	if !strings.Contains(summary, "Celana Dalam Boxer") {
		t.Fatalf("summary missing product: %s", summary)
	}
}
