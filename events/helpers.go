package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"encore.dev/beta/auth"
	"encore.dev"
	"golang.org/x/sync/singleflight"

	appcrypto "encore.app/wabantu/shared/crypto"
	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/pii"
	"encore.app/wabantu/shared/tenantschema"
	"encore.app/wabantu/shared/types"
	"encore.app/wabantu/tenant"
)

const (
	maxPatientExportRows   = 2500
	maxPatientListPageSize = 100
	maxPersonNameLen       = 200
	maxPatientNameLen      = 200
	maxComplaintLen        = 2000
	maxNotesLen            = 1000
	maxPreferredTimeLen    = 120
	maxPatientSearchQLen   = 100
)

var secrets struct {
	DataEncryptionKey string
	AnthropicAPIKey   string
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func mustUser(ctx context.Context) (*types.AuthUser, error) {
	data := auth.Data()
	if data == nil {
		return nil, appErrs.Unauthenticated("not authenticated")
	}
	u, ok := data.(*types.AuthUser)
	if !ok || !u.HasEffectiveTenantContext() {
		return nil, appErrs.Forbidden("tenant context required")
	}
	return u, nil
}

func assertOwner(u *types.AuthUser) error {
	if !u.CanPerformOwnerActions() {
		return appErrs.Forbidden("only owner can perform this action")
	}
	return nil
}

func isOwner(u *types.AuthUser) bool {
	return u.CanPerformOwnerActions()
}

var (
	eventsSchemaMu     sync.Mutex
	eventsSchemaDone   = make(map[string]bool)
	eventsSchemaGroup  singleflight.Group
)

// ensureEventsSchema applies evt_* DDL on tenants created before the events module (idempotent).
func ensureEventsSchema(ctx context.Context, schema string) error {
	eventsSchemaMu.Lock()
	done := eventsSchemaDone[schema]
	eventsSchemaMu.Unlock()
	if done {
		ready, err := eventsPersonPatchReady(ctx, schema)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		eventsSchemaMu.Lock()
		delete(eventsSchemaDone, schema)
		eventsSchemaMu.Unlock()
	}

	// Hot path: skip TenantConn / SET search_path when migration already applied.
	moduleReady, err := tenantschema.EventsModuleReady(ctx, tenant.DataDB.Stdlib(), schema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	if moduleReady {
		eventsSchemaMu.Lock()
		eventsSchemaDone[schema] = true
		eventsSchemaMu.Unlock()
		return nil
	}

	_, sfErr, _ := eventsSchemaGroup.Do(schema, func() (any, error) {
		eventsSchemaMu.Lock()
		if eventsSchemaDone[schema] {
			eventsSchemaMu.Unlock()
			return nil, nil
		}
		eventsSchemaMu.Unlock()

		if err := applyEventsSchemaPatches(ctx, schema); err != nil {
			return nil, err
		}

		eventsSchemaMu.Lock()
		eventsSchemaDone[schema] = true
		eventsSchemaMu.Unlock()
		return nil, nil
	})
	return sfErr
}

func applyEventsSchemaPatches(ctx context.Context, schema string) error {
	conn, err := tenant.TenantConn(ctx, schema)
	if err != nil {
		return err
	}
	defer tenant.CloseTenantConn(conn)
	exists, err := tenantschema.TableExistsConn(ctx, conn, "evt_event")
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	cloudReady, err := tenantschema.CloudTenantReadyConn(ctx, conn)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	var patchErr error
	// Cloud tenants with evt_event must still run idempotent evt_* DDL (new columns/indexes).
	if !exists && !cloudReady {
		patchErr = tenant.RunSchemaPatches(ctx, schema)
	} else {
		patchErr = tenant.RunEventsSchemaPatches(ctx, schema)
	}
	if patchErr != nil {
		return patchErr
	}
	if seedErr := tenant.SeedEventsMasterDataOnly(ctx, schema); seedErr != nil {
		return seedErr
	}
	if exists {
		if err := ensureEventsMissingColumns(ctx, conn, schema); err != nil {
			return err
		}
	}
	return nil
}

func eventsPersonPatchReady(ctx context.Context, schema string) (bool, error) {
	db := tenant.DataDB.Stdlib()
	exists, err := tenantschema.TableExists(ctx, db, schema, "evt_event_person")
	if err != nil {
		return false, appErrs.Internal(err.Error())
	}
	if !exists {
		return true, nil
	}
	hasMeals, err := tenantschema.ColumnExists(ctx, db, schema, "evt_event_person", "counts_toward_meals")
	if err != nil {
		return false, appErrs.Internal(err.Error())
	}
	if !hasMeals {
		return false, nil
	}
	hasCatering, err := tenantschema.ColumnExists(ctx, db, schema, "evt_event", "catering_order_notes")
	if err != nil {
		return false, appErrs.Internal(err.Error())
	}
	return hasCatering, nil
}

func ensureEventsMissingColumns(ctx context.Context, conn *sql.Conn, schema string) error {
	type colCheck struct {
		table  string
		column string
	}
	checks := []colCheck{
		{"evt_event", "break_start_time"},
		{"evt_event", "catering_order_notes"},
		{"evt_event_person", "counts_toward_meals"},
	}
	var missing *colCheck
	for i := range checks {
		c := &checks[i]
		has, colErr := tenantschema.ColumnExistsConn(ctx, conn, c.table, c.column)
		if colErr != nil {
			return appErrs.Internal(colErr.Error())
		}
		if !has {
			missing = c
			break
		}
	}
	if missing == nil {
		return nil
	}
	if encore.Meta().Environment.Cloud != encore.CloudLocal {
		if err := tenant.EnsureCloudAdminTenantDDL(ctx, schema); err != nil {
			return err
		}
	}
	return tenant.RunEventsSchemaPatches(ctx, schema)
}

func paginate(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > maxPatientListPageSize {
		pageSize = maxPatientListPageSize
	}
	return page, pageSize
}

func clampLen(s string, max int) string {
	s = strings.TrimSpace(s)
	if max > 0 && len(s) > max {
		return s[:max]
	}
	return s
}

func assertEventExists(ctx context.Context, u *types.AuthUser, ts tenantScope, eventID string) error {
	var one int
	err := ts.QueryRowContext(ctx, `
		SELECT 1 FROM evt_event WHERE id=$1::uuid AND deleted_at IS NULL`, eventID,
	).Scan(&one)
	if err == sql.ErrNoRows {
		return eventAccessErr(ctx, u, eventID, ts.Sch.Schema)
	}
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	return nil
}

func validatePatientFilters(f patientFilterInput) error {
	if len(strings.TrimSpace(f.Q)) > maxPatientSearchQLen {
		return appErrs.BadRequest("kata kunci pencarian terlalu panjang")
	}
	if st := strings.TrimSpace(f.Status); st != "" {
		st = strings.ToUpper(st)
		if st != "CONFIRMED" && st != "CANCELLED" && st != "COMPLETED" {
			return appErrs.BadRequest("status filter tidak valid")
		}
	}
	if sd := strings.TrimSpace(f.SlotDate); sd != "" {
		if _, err := time.Parse("2006-01-02", sd); err != nil {
			return appErrs.BadRequest("format tanggal slot tidak valid")
		}
	}
	switch strings.ToLower(strings.TrimSpace(f.HasSlot)) {
	case "", "true", "false":
	default:
		return appErrs.BadRequest("filter jadwal tidak valid")
	}
	return nil
}

func isDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "idx_evt_patient_dup")
}

func offsetLimit(page, pageSize int) (int, int) {
	return (page - 1) * pageSize, pageSize
}

func normalizePatientName(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace && b.Len() > 0 {
				b.WriteRune(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

func normalizeBirthDate(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", appErrs.BadRequest("tanggal lahir wajib diisi")
	}
	layouts := []string{"2006-01-02", "02/01/2006", "02-01-2006", "2/1/2006"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02"), nil
		}
	}
	return "", appErrs.BadRequest("format tanggal lahir tidak valid (gunakan YYYY-MM-DD)")
}

func encryptPatientField(plain string) (string, error) {
	key := strings.TrimSpace(secrets.DataEncryptionKey)
	if len(key) < 32 {
		return "", appErrs.Internal("encryption key not configured")
	}
	return appcrypto.Encrypt(strings.TrimSpace(plain), key)
}

func decryptPatientField(enc string) (string, error) {
	key := strings.TrimSpace(secrets.DataEncryptionKey)
	if len(key) < 32 {
		return "", appErrs.Internal("encryption key not configured")
	}
	return appcrypto.Decrypt(enc, key)
}

func patientBlindName(fullName string) string {
	return pii.BlindIndex(normalizePatientName(fullName), strings.TrimSpace(secrets.DataEncryptionKey))
}

func patientBlindBirth(birth string) string {
	return pii.BlindIndex(strings.TrimSpace(birth), strings.TrimSpace(secrets.DataEncryptionKey))
}

func encryptPersonName(plain string) (enc string, blindIdx string, err error) {
	key := strings.TrimSpace(secrets.DataEncryptionKey)
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "", "", nil
	}
	enc, err = pii.Encrypt(plain, key)
	if err != nil {
		return "", "", err
	}
	blindIdx = pii.BlindIndex(pii.NormalizeName(plain), key)
	return enc, blindIdx, nil
}

func decryptPersonName(enc, legacy string) (string, error) {
	return pii.DecryptOrLegacy(enc, legacy, strings.TrimSpace(secrets.DataEncryptionKey))
}

func piiPlaceholder(enc string) string {
	if strings.TrimSpace(enc) == "" {
		return ""
	}
	return pii.Placeholder
}

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "event"
	}
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

func auditEvent(ctx context.Context, ts tenantScope, u *types.AuthUser, entityType, entityID, action string, before, after any) {
	var actorID, role *string
	if u != nil {
		actorID = &u.AccountID
		role = &u.Role
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	_, _ = ts.ExecContext(ctx, `
		INSERT INTO evt_audit_log (entity_type, entity_id, action, actor_id, actor_role, before_data, after_data)
		VALUES ($1,$2::uuid,$3,$4::uuid,$5,$6::jsonb,$7::jsonb)`,
		entityType, entityID, action, actorID, role, nullJSON(beforeJSON), nullJSON(afterJSON),
	)
}

func nullJSON(b []byte) interface{} {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	return string(b)
}

func eventStatusAllowsPublicReg(status string) bool {
	return status == "PUBLISHED"
}

func eventStatusAllowsPublicView(status string) bool {
	switch status {
	case "PUBLISHED", "CLOSED", "CANCELLED":
		return true
	default:
		return false
	}
}

func registrationOpen(now time.Time, openAt, closeAt sql.NullTime) bool {
	if openAt.Valid && now.Before(openAt.Time) {
		return false
	}
	if closeAt.Valid && now.After(closeAt.Time) {
		return false
	}
	return true
}

func assertEventMutable(ctx context.Context, ts tenantScope, eventID string) error {
	var status string
	err := ts.QueryRowContext(ctx, `
		SELECT status FROM evt_event WHERE id=$1::uuid AND deleted_at IS NULL`, eventID,
	).Scan(&status)
	if err == sql.ErrNoRows {
		return appErrs.NotFound("acara tidak ditemukan")
	}
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	if strings.ToUpper(status) == "ARCHIVED" {
		return appErrs.BadRequest("acara diarsipkan — tidak dapat diubah")
	}
	return nil
}

func uniqueSlug(ctx context.Context, ts tenantScope, base string, excludeID string) (string, error) {
	candidate := base
	for i := 0; i < 50; i++ {
		var exists bool
		q := `SELECT EXISTS(SELECT 1 FROM evt_event WHERE event_slug=$1 AND deleted_at IS NULL`
		args := []any{candidate}
		if excludeID != "" {
			q += ` AND id <> $2::uuid`
			args = append(args, excludeID)
		}
		q += `)`
		if err := ts.QueryRowContext(ctx, q, args...).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i+2)
	}
	return "", appErrs.Internal("could not generate unique slug")
}
