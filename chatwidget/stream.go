package chatwidget

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"encore.app/wabantu/shared/inboxrealtime"
)

func setChatStreamCORS(w http.ResponseWriter, r *http.Request) {
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
}

type streamPath struct {
	TenantSlug string
	SessionID  string
}

func parseStreamPath(path string) streamPath {
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	// /api/v1/public/chat/:tenantSlug/sessions/:sessionId/stream
	if len(parts) < 8 {
		return streamPath{}
	}
	return streamPath{TenantSlug: parts[4], SessionID: parts[6]}
}

// PublicChatStream streams typing/done events for a web chat session (MVP: keep-alive + done).
//
//encore:api public raw method=GET path=/api/v1/public/chat/:tenantSlug/sessions/:sessionId/stream
func PublicChatStream(w http.ResponseWriter, r *http.Request) {
	setChatStreamCORS(w, r)
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	p := parseStreamPath(r.URL.Path)
	token := strings.TrimSpace(r.URL.Query().Get("visitorToken"))
	if p.TenantSlug == "" || p.SessionID == "" || token == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !verifyVisitorToken(secrets.JWTSecret, p.TenantSlug, p.SessionID, token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	writeEvent := func(event string, payload any) {
		b, _ := json.Marshal(payload)
		if event != "" {
			_, _ = w.Write([]byte("event: " + event + "\n"))
		}
		_ = inboxrealtime.WriteSSE(w, b)
		flusher.Flush()
	}

	writeEvent("typing", map[string]bool{"active": true})

	ctx := r.Context()
	pingTicker := time.NewTicker(25 * time.Second)
	defer pingTicker.Stop()

	deadline := time.NewTimer(55 * time.Second)
	defer deadline.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			writeEvent("done", map[string]any{"usage": map[string]int{"tokens": 0}})
			return
		case <-pingTicker.C:
			ping, _ := json.Marshal(inboxrealtime.PingPayload{Type: "ping"})
			_ = inboxrealtime.WriteSSE(w, ping)
			flusher.Flush()
		}
	}
}
