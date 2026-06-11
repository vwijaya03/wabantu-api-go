// Backfill legacy plaintext PII into encrypted columns for one tenant schema.
//
// Usage:
//
//	DATABASE_URL="$(encore db conn-uri tenant --write --env=local)" \
//	DATA_ENCRYPTION_KEY="..." \
//	go run ./scripts/cmd/backfill-pii/ -schema=t_xxx
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"

	"encore.app/wabantu/shared/pii"
	"encore.app/wabantu/shared/tenantschema"
)

func main() {
	schema := flag.String("schema", "", "tenant schema (required)")
	flag.Parse()
	if strings.TrimSpace(*schema) == "" {
		fmt.Fprintln(os.Stderr, "usage: backfill-pii -schema=t_xxx")
		os.Exit(1)
	}
	uri := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	key := strings.TrimSpace(os.Getenv("DATA_ENCRYPTION_KEY"))
	if uri == "" || key == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL and DATA_ENCRYPTION_KEY required")
		os.Exit(1)
	}
	if err := pii.ValidateKey(key); err != nil {
		fmt.Fprintln(os.Stderr, "invalid DATA_ENCRYPTION_KEY:", err)
		os.Exit(1)
	}

	db, err := sql.Open("pgx", uri)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`SET search_path TO %q, public`, *schema)); err != nil {
		fmt.Fprintln(os.Stderr, "set search_path:", err)
		os.Exit(1)
	}

	ready, err := tenantschema.TableColumnExists(ctx, conn, *schema, "contact", "phone_number_idx")
	if err != nil || !ready {
		fmt.Fprintln(os.Stderr, "PII columns missing — run apply-pii-schema first")
		os.Exit(1)
	}

	for round := 0; round < 20; round++ {
		if err := pii.BackfillTenant(ctx, conn, key); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		var remaining int
		_ = conn.QueryRowContext(ctx, `
			SELECT (
			  (SELECT count(*) FROM contact WHERE deleted_at IS NULL
			    AND (phone_number_enc IS NULL OR phone_number_enc = '')
			    AND NULLIF(TRIM(phone_number), '') IS NOT NULL AND phone_number <> $1)
			+ (SELECT count(*) FROM lead WHERE deleted_at IS NULL
			    AND (phone_number_enc IS NULL OR phone_number_enc = '')
			    AND NULLIF(TRIM(phone_number), '') IS NOT NULL AND phone_number <> $1)
			)`, pii.Placeholder).Scan(&remaining)
		if remaining == 0 {
			break
		}
		if round == 19 {
			fmt.Fprintf(os.Stderr, "warning: %d contact/lead rows still plaintext after backfill\n", remaining)
			break
		}
	}
	fmt.Println("backfill complete for", *schema)
}
