package apitest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"encore.app/wabantu/auth"
)

type authSuccessEnvelope struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		AccessToken      string `json:"accessToken"`
		ExpiresInSeconds int    `json:"expiresInSeconds"`
		User             struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Name  string `json:"name"`
			Role  string `json:"role"`
			Tenant *struct {
				ID   string `json:"id"`
				Slug string `json:"slug"`
				Name string `json:"name"`
			} `json:"tenant"`
		} `json:"user"`
	} `json:"data"`
}

type meSuccessEnvelope struct {
	Success bool `json:"success"`
	Data    struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
		Role  string `json:"role"`
		Tenant *struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
			Name string `json:"name"`
		} `json:"tenant"`
	} `json:"data"`
}

func TestAuthSmoke_Register(t *testing.T) {
	BootstrapOwner(t)
}

func TestAuthSmoke_Login(t *testing.T) {
	RequireRedis(t)
	fx := BootstrapOwner(t)

	rr := httptest.NewRecorder()
	req := NewJSONPostRequest(t, "/api/v1/auth/login", map[string]string{
		"email":    fx.Email,
		"password": fx.Password,
	})
	auth.ServeLoginHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp authSuccessEnvelope
	DecodeJSON(t, rr, &resp)
	if !resp.Success {
		t.Fatalf("login success=false: %s", resp.Message)
	}
	if resp.Data.AccessToken == "" {
		t.Fatal("login: missing accessToken")
	}
	if resp.Data.User.Email != fx.Email {
		t.Fatalf("login user email = %q, want %q", resp.Data.User.Email, fx.Email)
	}
}

func TestAuthSmoke_Me(t *testing.T) {
	RequireRedis(t)
	fx := BootstrapOwnerWithToken(t)

	rr := httptest.NewRecorder()
	req := NewGetRequest("/api/v1/auth/me", BearerHeader(fx.Token))
	auth.ServeMeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("me status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp meSuccessEnvelope
	DecodeJSON(t, rr, &resp)
	if !resp.Success {
		t.Fatal("me success=false")
	}
	if resp.Data.Email != fx.Email {
		t.Fatalf("me email = %q, want %q", resp.Data.Email, fx.Email)
	}
	if resp.Data.Tenant == nil || strings.TrimSpace(resp.Data.Tenant.Slug) == "" {
		t.Fatal("me: expected tenant in profile")
	}
}

func TestAuthSmoke_LoginHelper(t *testing.T) {
	fx := BootstrapOwner(t)
	loggedIn := LoginOwner(t, fx.Email, fx.Password)
	if loggedIn.Token == "" {
		t.Fatal("LoginOwner: missing token")
	}
	if loggedIn.Email != fx.Email {
		t.Fatalf("LoginOwner email = %q, want %q", loggedIn.Email, fx.Email)
	}
}
