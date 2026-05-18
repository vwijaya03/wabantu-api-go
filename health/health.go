package health

import (
	"context"

	"encore.dev/storage/sqldb"
)

var systemDB = sqldb.Named("system")
var tenantDB = sqldb.Named("tenant")

type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

type ReadyResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
	SystemDB string `json:"systemDb"`
	TenantDB string `json:"tenantDb"`
}

// Health is a lightweight liveness probe.
//
//encore:api public method=GET path=/api/v1/health
func Health(ctx context.Context) (*HealthResponse, error) {
	return &HealthResponse{Status: "ok", Service: "wabantu-api"}, nil
}

// Ready checks connectivity to both system and tenant databases.
//
//encore:api public method=GET path=/api/v1/health/ready
func Ready(ctx context.Context) (*ReadyResponse, error) {
	sysOK := systemDB.QueryRow(ctx, `SELECT 1`).Scan(new(int)) == nil
	tenOK := tenantDB.QueryRow(ctx, `SELECT 1`).Scan(new(int)) == nil

	sysStatus := "connected"
	if !sysOK {
		sysStatus = "unavailable"
	}
	tenStatus := "connected"
	if !tenOK {
		tenStatus = "unavailable"
	}

	overall := "ok"
	dbLabel := "connected"
	if !sysOK || !tenOK {
		overall = "degraded"
		dbLabel = "degraded"
	}

	return &ReadyResponse{
		Status:   overall,
		Database: dbLabel,
		SystemDB: sysStatus,
		TenantDB: tenStatus,
	}, nil
}
