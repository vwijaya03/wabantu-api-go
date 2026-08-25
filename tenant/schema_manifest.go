package tenant

// PatchManifest documents a schema patch release. RequiresAdminDDL means cloud
// operators must run scripts/apply-tenant-schema-cloud.sh before app workers succeed.
type PatchManifest struct {
	Version          int
	RequiresAdminDDL bool
	Description      string
}

// SchemaManifests lists known patch versions (newest last).
var SchemaManifests = []PatchManifest{
	{
		Version:          CurrentSchemaPatchVersion,
		RequiresAdminDDL: false,
		Description:      "Payment proof, inventory, finance — applied via migrate-tenant-schemas on cloud",
	},
}

// manifestForVersion returns the manifest entry for v, or nil.
func manifestForVersion(v int) *PatchManifest {
	for i := range SchemaManifests {
		if SchemaManifests[i].Version == v {
			return &SchemaManifests[i]
		}
	}
	return nil
}
