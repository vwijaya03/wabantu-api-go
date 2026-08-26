package apitest

import (
	"testing"

	"encore.app/wabantu/health"
)

func TestHealthSmoke_Liveness(t *testing.T) {
	resp, err := health.Health(t.Context())
	if err != nil {
		t.Fatalf("GET /api/v1/health: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q, want ok", resp.Status)
	}
	if resp.Service != "wabantu-api" {
		t.Fatalf("service = %q, want wabantu-api", resp.Service)
	}
}

func TestHealthSmoke_Readiness(t *testing.T) {
	RequireEncoreInfra(t)

	resp, err := health.Ready(t.Context())
	if err != nil {
		t.Fatalf("GET /api/v1/health/ready: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q, want ok (systemDb=%s tenantDb=%s)", resp.Status, resp.SystemDB, resp.TenantDB)
	}
	if resp.Database != "connected" {
		t.Fatalf("database = %q, want connected", resp.Database)
	}
	if resp.SystemDB != "connected" || resp.TenantDB != "connected" {
		t.Fatalf("db connectivity: systemDb=%s tenantDb=%s", resp.SystemDB, resp.TenantDB)
	}
}
