package whatsapp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestMarkReadWithTypingRequest(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()
	old := graphBaseURL
	graphBaseURL = srv.URL
	defer func() { graphBaseURL = old }()

	if err := MarkReadWithTyping(context.Background(), " tok ", "12345", "wamid.ABC"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/"+GraphAPIVersion+"/12345/messages" || gotAuth != "Bearer tok" {
		t.Fatalf("path=%q auth set=%v", gotPath, gotAuth == "Bearer tok")
	}
	want := map[string]any{
		"messaging_product": "whatsapp",
		"status":            "read",
		"message_id":        "wamid.ABC",
		"typing_indicator":  map[string]any{"type": "text"},
	}
	if !reflect.DeepEqual(gotBody, want) {
		t.Fatalf("body = %#v", gotBody)
	}
}

func TestMarkReadWithTypingErrors(t *testing.T) {
	if err := MarkReadWithTyping(context.Background(), "tok", "1", ""); err == nil {
		t.Fatal("missing message id must error")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
	}))
	defer srv.Close()
	old := graphBaseURL
	graphBaseURL = srv.URL
	defer func() { graphBaseURL = old }()
	if err := MarkReadWithTyping(context.Background(), "tok", "1", "wamid.X"); err == nil {
		t.Fatal("non-2xx must error")
	}
}
