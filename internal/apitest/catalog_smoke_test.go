package apitest

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"testing"

	"encore.app/wabantu/broadcast"
	"encore.app/wabantu/tenant"
	"encore.app/wabantu/tenantaccess"
)

//go:embed catalog_snapshot.json
var catalogSnapshotJSON []byte

const expectedEndpointCount = 349

type catalogEndpoint struct {
	Service    string `json:"service"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Annotation string `json:"annotation"`
}

type catalogService struct {
	EndpointCount int               `json:"endpointCount"`
	Endpoints     []catalogEndpoint `json:"endpoints"`
}

type catalogSnapshot struct {
	EndpointCount int                       `json:"endpointCount"`
	ServiceCount  int                       `json:"serviceCount"`
	Services      map[string]catalogService `json:"services"`
}

func loadCatalog(t *testing.T) catalogSnapshot {
	t.Helper()
	var snap catalogSnapshot
	if err := json.Unmarshal(catalogSnapshotJSON, &snap); err != nil {
		t.Fatalf("parse catalog_snapshot.json: %v", err)
	}
	return snap
}

// serviceSmokePhase tracks per-service HTTP smoke coverage (regenerate catalog via scripts/gen-apiregistry-catalog.sh).
var serviceSmokePhase = map[string]struct {
	phase  int
	status string // covered | pending
	reason string
}{
	"admin":        {1, "covered", "admin_audit_smoke_test.go"},
	"ai":           {1, "covered", "remaining_smoke_test.go"},
	"analytics":    {1, "covered", "analytics_smoke_test.go"},
	"audit":        {1, "covered", "admin_audit_smoke_test.go"},
	"auth":         {1, "covered", "auth_smoke_test.go"},
	"billing":      {1, "covered", "billing_smoke_test.go"},
	"branch":       {1, "covered", "branch_smoke_test.go"},
	"broadcast":    {1, "covered", "catalog_smoke_test.go"},
	"business":     {1, "covered", "business_smoke_test.go"},
	"events":       {1, "covered", "events_smoke_test.go"},
	"finance":      {1, "covered", "finance_smoke_test.go"},
	"flag":         {1, "covered", "remaining_smoke_test.go"},
	"health":       {1, "covered", "health_smoke_test.go"},
	"importcsv":    {1, "covered", "importcsv_smoke_test.go + importcsv/import_test.go"},
	"inbox":        {1, "covered", "inbox_smoke_test.go"},
	"internal":     {0, "pending", "internal/apiregistry — bukan service Encore publik"},
	"inventory":    {1, "covered", "inventory_smoke_test.go (subset)"},
	"kb":           {1, "covered", "kb_smoke_test.go"},
	"leads":        {1, "covered", "leads_smoke_test.go"},
	"notification": {1, "covered", "remaining_smoke_test.go"},
	"order":        {1, "covered", "order_smoke_test.go"},
	"payment":      {1, "covered", "payment_webhook_smoke_test.go (webhook only)"},
	"scripts":      {0, "pending", "scripts/gen-api-catalog.go — bukan service Encore"},
	"shipping":     {1, "covered", "remaining_smoke_test.go"},
	"tenant":       {1, "covered", "catalog_smoke_test.go"},
	"tenantaccess": {1, "covered", "catalog_smoke_test.go"},
	"usage":        {1, "covered", "usage_smoke_test.go"},
	"webhook":      {1, "covered", "webhook_smoke_test.go"},
	"whatsappapi":  {1, "covered", "remaining_smoke_test.go"},
	"workflow":     {1, "covered", "workflow_smoke_test.go"},
}

func TestCatalog_EndpointInventory(t *testing.T) {
	snap := loadCatalog(t)
	if snap.EndpointCount != expectedEndpointCount {
		t.Fatalf("catalog endpointCount=%d want %d (regenerate: ./scripts/gen-apiregistry-catalog.sh)", snap.EndpointCount, expectedEndpointCount)
	}
	if len(snap.Services) != snap.ServiceCount {
		t.Fatalf("serviceCount mismatch: header=%d map=%d", snap.ServiceCount, len(snap.Services))
	}
}

func TestCatalog_AllServicesRegistered(t *testing.T) {
	snap := loadCatalog(t)
	for svc, meta := range snap.Services {
		if meta.EndpointCount != len(meta.Endpoints) {
			t.Fatalf("service %q endpointCount=%d len(endpoints)=%d", svc, meta.EndpointCount, len(meta.Endpoints))
		}
		for _, ep := range meta.Endpoints {
			if ep.Service != svc {
				t.Fatalf("endpoint service mismatch: bucket=%q entry=%q file=%s", svc, ep.Service, ep.File)
			}
			if ep.Annotation == "" {
				t.Fatalf("empty annotation for %s:%d", ep.File, ep.Line)
			}
		}
		phase, ok := serviceSmokePhase[svc]
		if !ok {
			t.Fatalf("service %q missing from serviceSmokePhase map (%d endpoints)", svc, meta.EndpointCount)
		}
		if phase.status != "covered" && phase.status != "pending" {
			t.Fatalf("service %q invalid status %q", svc, phase.status)
		}
	}
	for svc := range serviceSmokePhase {
		if _, ok := snap.Services[svc]; !ok {
			t.Fatalf("serviceSmokePhase has unknown service %q", svc)
		}
	}
}

func TestCatalog_ServiceSmokeCoverage(t *testing.T) {
	snap := loadCatalog(t)
	var covered, pending int
	for svc, meta := range snap.Services {
		phase := serviceSmokePhase[svc]
		name := fmt.Sprintf("%s/%d_endpoints/phase%d_%s", svc, meta.EndpointCount, phase.phase, phase.status)
		t.Run(name, func(t *testing.T) {
			switch phase.status {
			case "covered":
				covered++
			case "pending":
				pending++
				t.Skipf("phase %d pending: %s", phase.phase, phase.reason)
			default:
				t.Fatalf("unknown status %q", phase.status)
			}
		})
	}
	t.Logf("catalog: %d endpoints, %d services — smoke covered=%d pending=%d",
		snap.EndpointCount, len(snap.Services), covered, pending)
}

func TestTenantAccessSmoke_ListRequests(t *testing.T) {
	RequireEncoreInfra(t)
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)
	ctx := context.Background()
	out, err := tenantaccess.ListTenantRequests(ctx)
	if err != nil {
		t.Fatalf("tenantaccess.ListTenantRequests: %v", err)
	}
	AssertJSONFields(t, out, "requests")
}

func TestTenantSmoke_Readiness(t *testing.T) {
	RequireEncoreInfra(t)
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)
	ctx := context.Background()
	out, err := tenant.GetTenantReadiness(ctx)
	if err != nil {
		t.Fatalf("tenant.GetTenantReadiness: %v", err)
	}
	AssertJSONFields(t, out, "ready", "baseProvisioned", "cloudReady")
}

func TestBroadcastSmoke_ListCampaigns(t *testing.T) {
	RequireEncoreInfra(t)
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)
	ctx := context.Background()
	out, err := broadcast.ListCampaigns(ctx)
	if err != nil {
		t.Fatalf("broadcast.ListCampaigns: %v", err)
	}
	AssertJSONArrayField(t, out, "campaigns")
}
