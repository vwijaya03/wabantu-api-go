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
		Version:     2,
		Description: "Payment proof, inventory, finance, RAG retrieval",
	},
	{
		Version:          CurrentSchemaPatchVersion,
		RequiresAdminDDL: false,
		Description:      "Web chat widget tables (chat_widget_config, web_chat_session, web_chat_message)",
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
