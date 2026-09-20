package admin

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"encore.dev/beta/errs"
)

// CompleteAITriageJobResponse is the GHA callback ACK (behavior jobs).
type CompleteAITriageJobResponse struct {
	OK bool `json:"ok"`
}

func triageJobIDFromPath(req *http.Request) string {
	if id := triagePathUUID(req.PathValue("id")); id != "" {
		return id
	}
	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	for i, p := range parts {
		if i+1 >= len(parts) {
			continue
		}
		if p != "behavior-jobs" {
			continue
		}
		if id := triagePathUUID(parts[i+1]); id != "" {
			return id
		}
	}
	return ""
}

func triagePathUUID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "complete" {
		return ""
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return ""
	}
	return id.String()
}

func requireTriageUUID(id string) (string, error) {
	parsed := triagePathUUID(id)
	if parsed == "" {
		return "", &errs.Error{Code: errs.InvalidArgument, Message: "id tidak valid"}
	}
	return parsed, nil
}

func assertTriageInternalToken(token string) error {
	expected := strings.TrimSpace(secrets.AiInternalToken)
	if expected == "" || token == "" {
		return &errs.Error{Code: errs.Unauthenticated, Message: "Unauthorized internal request"}
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		return &errs.Error{Code: errs.Unauthenticated, Message: "Unauthorized internal request"}
	}
	return nil
}

func writeTriageJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeTriageJSONError(w http.ResponseWriter, err error) {
	if e, ok := err.(*errs.Error); ok {
		status := http.StatusInternalServerError
		switch e.Code {
		case errs.Unauthenticated:
			status = http.StatusUnauthorized
		case errs.InvalidArgument:
			status = http.StatusBadRequest
		case errs.NotFound:
			status = http.StatusNotFound
		}
		writeTriageJSON(w, status, map[string]string{"message": e.Message})
		return
	}
	writeTriageJSON(w, http.StatusInternalServerError, map[string]string{"message": "internal error"})
}
