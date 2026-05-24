// One-off maintenance: apply tenant schema patches (incl. finance tables).
//
// Run (with encore run already up, or let exec start infra):
//
//	encore exec ./cmd/migrate-tenant-schemas
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"encore.app/wabantu/tenant"
)

func main() {
	resp, err := tenant.RunMigrateAllTenantSchemas(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate failed: %v\n", err)
		os.Exit(1)
	}
	out, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(out))
}
