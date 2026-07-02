package tenant

import "testing"

func TestShouldUseSyncMigration(t *testing.T) {
	if ShouldUseSyncMigration(nil) {
		t.Fatal("nil request should be async")
	}
	if ShouldUseSyncMigration(&MigrateSchemasRequest{}) {
		t.Fatal("empty ids should be async")
	}
	if !ShouldUseSyncMigration(&MigrateSchemasRequest{TenantIDs: []string{"a"}}) {
		t.Fatal("1 tenant should sync")
	}
	if !ShouldUseSyncMigration(&MigrateSchemasRequest{TenantIDs: []string{"a", "b", "c"}}) {
		t.Fatal("3 tenants should sync")
	}
	if ShouldUseSyncMigration(&MigrateSchemasRequest{TenantIDs: []string{"a", "b", "c", "d"}}) {
		t.Fatal("4 tenants should be async")
	}
}

func TestManifestForVersion(t *testing.T) {
	m := manifestForVersion(CurrentSchemaPatchVersion)
	if m == nil {
		t.Fatal("expected manifest for current version")
	}
	if m.Version != CurrentSchemaPatchVersion {
		t.Fatalf("version mismatch: %d", m.Version)
	}
}
