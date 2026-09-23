package publictenant

// TenantRef is a resolved public tenant slug.
type TenantRef struct {
	TenantID     string
	TenantSchema string
	Slug         string
}
