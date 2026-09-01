package tenant

// CurrentSchemaPatchVersion is bumped whenever tenant/schema_patch.go (or related
// always-apply patches) changes. Workers only process tenants below this version.
const CurrentSchemaPatchVersion = 2
