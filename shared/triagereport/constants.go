package triagereport

const (
	StatusOpen       = "open"
	StatusConfirmed  = "confirmed"
	StatusDismissed  = "dismissed"

	ReporterRoleTenantUser  = "tenant_user"
	ReporterRoleSuperAdmin  = "super_admin"

	CategoryWrongAnswer = "wrong_answer"
	CategoryBug         = "bug"
	CategoryRude        = "rude"
	CategoryOffTopic    = "off_topic"
	CategoryOther       = "other"

	TenantDailyLimit     = 20
	SuperAdminDailyLimit = 50

	MaxReporterNoteLen = 500
)

var ValidCategories = map[string]bool{
	CategoryWrongAnswer: true,
	CategoryBug:         true,
	CategoryRude:        true,
	CategoryOffTopic:    true,
	CategoryOther:       true,
}

var ValidStatuses = map[string]bool{
	StatusOpen:      true,
	StatusConfirmed: true,
	StatusDismissed: true,
}

func DailyLimitForRole(role string) int {
	if role == ReporterRoleSuperAdmin {
		return SuperAdminDailyLimit
	}
	return TenantDailyLimit
}

func ReporterRoleFromAuth(role string) string {
	if role == "super_admin" {
		return ReporterRoleSuperAdmin
	}
	return ReporterRoleTenantUser
}
