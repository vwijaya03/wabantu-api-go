package chatengine

import (
	"context"
	"testing"

	appdb "encore.app/wabantu/shared/db"
)

func TestProcessMessageDisabled(t *testing.T) {
	e := &Engine{}
	out, err := e.ProcessMessage(context.Background(), appdb.TenantScope{}, Input{Enabled: false, UserText: "halo"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Path != PathDisabled {
		t.Fatalf("path %s", out.Path)
	}
}
