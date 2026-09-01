package apitest

import (
	"context"
	"fmt"
	"testing"

	"encore.app/wabantu/tenant"
	"encore.app/wabantu/whatsappapi"
	appdb "encore.app/wabantu/shared/db"
)

func TestWhatsAppAPISmoke_DeleteChannelPermanent(t *testing.T) {
	RequireEncoreInfra(t)
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)
	ctx := context.Background()

	channelID := seedWhatsAppChannel(t, fx.SchemaName)

	out, err := whatsappapi.DeleteChannelPermanent(ctx, channelID)
	if err != nil {
		t.Fatalf("whatsappapi.DeleteChannelPermanent: %v", err)
	}
	if out.ID != channelID {
		t.Fatalf("DeleteChannelPermanent id = %q, want %q", out.ID, channelID)
	}

	var exists bool
	err = tenant.DataDB.Stdlib().QueryRowContext(ctx, fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s.whatsapp_channel WHERE id = $1::uuid)`,
		appdb.QuoteIdent(fx.SchemaName),
	), channelID).Scan(&exists)
	if err != nil {
		t.Fatalf("verify channel deleted: %v", err)
	}
	if exists {
		t.Fatal("whatsapp_channel row still exists after permanent delete")
	}
}

func seedWhatsAppChannel(t *testing.T, schema string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := tenant.DataDB.Stdlib().QueryRowContext(ctx, fmt.Sprintf(`
		INSERT INTO %s.whatsapp_channel (
			provider, display_name, phone_number, status, connected_at
		) VALUES ('meta_cloud', 'Apitest Channel', '+62819990001', 'disconnected', NOW())
		RETURNING id::text`,
		appdb.QuoteIdent(schema),
	)).Scan(&id)
	if err != nil {
		t.Fatalf("seed whatsapp_channel: %v", err)
	}
	return id
}
