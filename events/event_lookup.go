package events

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	appdb "encore.app/wabantu/shared/db"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/system"
	"encore.app/wabantu/tenant"
)

type eventOwnerInfo struct {
	SchemaName string
	TenantID   string
	TenantName string
	TenantSlug string
}

func lookupEventOwnerTenant(ctx context.Context, eventID, currentSchema string) (*eventOwnerInfo, error) {
	schemas, err := tenant.ListSchemaNames(ctx)
	if err != nil {
		return nil, err
	}
	conn := eventsDB.Stdlib()
	for _, schema := range schemas {
		if schema == currentSchema {
			continue
		}
		table := appdb.Qualify(schema, "evt_event")
		var one int
		err := conn.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT 1 FROM %s WHERE id=$1::uuid AND deleted_at IS NULL`, table),
			eventID,
		).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			continue
		}
		info := &eventOwnerInfo{SchemaName: schema}
		tenantID, err := tenant.TenantIDBySchema(ctx, schema)
		if err != nil {
			return info, nil
		}
		info.TenantID = tenantID
		var name, slug string
		err = system.DB.QueryRow(ctx,
			`SELECT name, slug FROM tenant WHERE id=$1::uuid AND deleted_at IS NULL`,
			tenantID,
		).Scan(&name, &slug)
		if err == nil {
			info.TenantName = name
			info.TenantSlug = slug
		}
		return info, nil
	}
	return nil, nil
}

func eventAccessErr(ctx context.Context, u *types.AuthUser, eventID, schema string) error {
	if u != nil && u.Role == "super_admin" {
		owner, err := lookupEventOwnerTenant(ctx, eventID, schema)
		if err != nil {
			return appErrs.Internal(err.Error())
		}
		if owner != nil {
			label := owner.TenantName
			if label == "" {
				label = owner.SchemaName
			}
			return appErrs.Forbidden(
				"acara milik tenant lain (" + label + "). Aktifkan mode Pantau tenant tersebut di konsol platform.",
			)
		}
	}
	return appErrs.NotFound("acara tidak ditemukan")
}
