package apitest

import (
	"context"
	"testing"

	"encore.app/wabantu/ai"
	"encore.app/wabantu/flag"
	"encore.app/wabantu/notification"
	"encore.app/wabantu/shipping"
	"encore.app/wabantu/whatsappapi"
)

func TestFlagSmoke_Check(t *testing.T) {
	RequireEncoreInfra(t)
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)
	ctx := context.Background()
	out, err := flag.CheckFlag(ctx, "apitest-nonexistent-flag")
	if err != nil {
		t.Fatalf("flag.CheckFlag: %v", err)
	}
	AssertJSONFields(t, out, "enabled")
}

func TestShippingSmoke_Provinces(t *testing.T) {
	RequireEncoreInfra(t)
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)
	ctx := context.Background()
	out, err := shipping.Provinces(ctx)
	if err != nil {
		t.Skipf("shipping.Provinces needs RajaOngkir secret/network: %v", err)
	}
	AssertJSONArrayField(t, out, "provinces")
}

func TestNotificationSmoke_List(t *testing.T) {
	RequireEncoreInfra(t)
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)
	ctx := context.Background()
	out, err := notification.List(ctx, &notification.ListParams{})
	if err != nil {
		t.Fatalf("notification.List: %v", err)
	}
	AssertJSONArrayField(t, out, "notifications")
}

func TestWhatsAppAPISmoke_ListChannels(t *testing.T) {
	RequireEncoreInfra(t)
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)
	ctx := context.Background()
	out, err := whatsappapi.ListChannels(ctx)
	if err != nil {
		t.Fatalf("whatsappapi.ListChannels: %v", err)
	}
	AssertJSONArrayField(t, out, "items")
}

func TestAISmoke_ListModelCatalog(t *testing.T) {
	RequireEncoreInfra(t)
	fx := BootstrapOwner(t)
	WithOwnerAuth(fx)
	ctx := context.Background()
	out, err := ai.ListModelCatalog(ctx)
	if err != nil {
		t.Fatalf("ai.ListModelCatalog: %v", err)
	}
	AssertJSONArrayField(t, out, "models")
}
