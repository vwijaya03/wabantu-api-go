package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"encore.dev/beta/errs"

	"encore.app/wabantu/shared/triageincident"
	"encore.app/wabantu/system"
)

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func insertOrLinkIncident(ctx context.Context, in triageincident.Incident, sourceType, sourceID, channel string) (triageincident.Incident, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.TenantSchema = strings.TrimSpace(in.TenantSchema)
	in.Fingerprint = strings.TrimSpace(in.Fingerprint)
	sourceID = strings.TrimSpace(sourceID)
	sourceType = strings.TrimSpace(sourceType)
	if in.TenantID == "" || in.TenantSchema == "" || in.Fingerprint == "" || sourceID == "" || sourceType == "" {
		return triageincident.Incident{}, &errs.Error{Code: errs.InvalidArgument, Message: "incident source tidak lengkap"}
	}
	if channel == "" {
		channel = "whatsapp"
	}

	var existingID string
	err := system.DB.QueryRow(ctx, `
		SELECT s.incident_id::text
		FROM ai_triage_incident_source s
		WHERE s.source_type = $1 AND s.source_id = $2
		LIMIT 1`, sourceType, sourceID).Scan(&existingID)
	if err == nil && existingID != "" {
		return loadIncident(ctx, existingID)
	}
	if err != nil && !isNoRows(err) {
		return triageincident.Incident{}, err
	}

	var openID string
	err = system.DB.QueryRow(ctx, `
		SELECT id::text FROM ai_triage_incident
		WHERE tenant_id = $1::uuid AND fingerprint = $2
		  AND review_status IN ('open', 'needs_human_input')
		ORDER BY created_at DESC LIMIT 1`, in.TenantID, in.Fingerprint).Scan(&openID)
	if err != nil && !isNoRows(err) {
		return triageincident.Incident{}, err
	}

	if openID == "" {
		ev := in.Evidence
		if len(ev) == 0 {
			ev = []byte("{}")
		}
		err = system.DB.QueryRow(ctx, `
			INSERT INTO ai_triage_incident (
				tenant_id, tenant_schema, channel, fingerprint, cross_channel_key,
				review_status, resolution_status, lane, degraded_mode,
				evidence_version, evidence_json, draft_contract_json
			) VALUES (
				$1::uuid, $2, $3, $4, $5,
				$6, $7, NULLIF($8, ''), NULLIF($9, ''),
				$10, $11::jsonb, $12::jsonb
			) RETURNING id::text`,
			in.TenantID, in.TenantSchema, channel, in.Fingerprint, in.CrossChannelKey,
			triageincident.ReviewOpen, triageincident.ResolutionNone, in.Lane, in.DegradedMode,
			in.EvidenceVersion, string(ev), nullJSON(in.DraftContract),
		).Scan(&openID)
		if err != nil {
			return triageincident.Incident{}, err
		}
	} else if len(in.Evidence) > 0 {
		// New report on the same fingerprint must show THIS turn, not an older sibling in the thread.
		_, _ = system.DB.Exec(ctx, `
			UPDATE ai_triage_incident
			SET evidence_json = $2::jsonb,
			    draft_contract_json = COALESCE($3::jsonb, draft_contract_json),
			    updated_at = NOW()
			WHERE id = $1::uuid
			  AND review_status IN ('open', 'needs_human_input')`,
			openID, string(in.Evidence), nullJSON(in.DraftContract),
		)
	}

	_, err = system.DB.Exec(ctx, `
		INSERT INTO ai_triage_incident_source (incident_id, source_type, source_id, channel)
		VALUES ($1::uuid, $2, $3, $4)
		ON CONFLICT (source_type, source_id) DO NOTHING`,
		openID, sourceType, sourceID, channel)
	if err != nil {
		return triageincident.Incident{}, err
	}
	return loadIncident(ctx, openID)
}

func loadIncidentBySource(ctx context.Context, sourceType, sourceID string) (triageincident.Incident, error) {
	var id string
	err := system.DB.QueryRow(ctx, `
		SELECT incident_id::text FROM ai_triage_incident_source
		WHERE source_type = $1 AND source_id = $2
		LIMIT 1`, sourceType, sourceID).Scan(&id)
	if isNoRows(err) {
		return triageincident.Incident{}, &errs.Error{Code: errs.NotFound, Message: "insiden belum ada untuk sumber ini"}
	}
	if err != nil {
		return triageincident.Incident{}, err
	}
	return loadIncident(ctx, id)
}

func nullJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}

func loadIncident(ctx context.Context, id string) (triageincident.Incident, error) {
	var inc triageincident.Incident
	var lane, degraded, jobID, repairID sql.NullString
	var draft, confirmed []byte
	err := system.DB.QueryRow(ctx, `
		SELECT id::text, tenant_id::text, tenant_schema, channel, fingerprint, cross_channel_key,
		       review_status, resolution_status, lane, degraded_mode, evidence_version,
		       evidence_json, draft_contract_json, confirmed_contract_json,
		       behavior_job_id::text, repair_plan_id::text, created_at, updated_at
		FROM ai_triage_incident WHERE id = $1::uuid`, id).Scan(
		&inc.ID, &inc.TenantID, &inc.TenantSchema, &inc.Channel, &inc.Fingerprint, &inc.CrossChannelKey,
		&inc.ReviewStatus, &inc.ResolutionStatus, &lane, &degraded, &inc.EvidenceVersion,
		&inc.Evidence, &draft, &confirmed,
		&jobID, &repairID, &inc.CreatedAt, &inc.UpdatedAt,
	)
	if isNoRows(err) {
		return triageincident.Incident{}, &errs.Error{Code: errs.NotFound, Message: "insiden tidak ditemukan"}
	}
	if err != nil {
		return triageincident.Incident{}, err
	}
	inc.Lane = lane.String
	inc.DegradedMode = degraded.String
	inc.DraftContract = draft
	inc.ConfirmedContract = confirmed
	inc.BehaviorJobID = jobID.String
	inc.RepairPlanID = repairID.String
	inc.Sources, _ = listIncidentSources(ctx, inc.ID)
	return inc, nil
}

func listIncidentSources(ctx context.Context, incidentID string) ([]triageincident.Source, error) {
	rows, err := system.DB.Query(ctx, `
		SELECT id::text, incident_id::text, source_type, source_id, channel, created_at
		FROM ai_triage_incident_source WHERE incident_id = $1::uuid ORDER BY created_at ASC`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []triageincident.Source
	for rows.Next() {
		var s triageincident.Source
		if err := rows.Scan(&s.ID, &s.IncidentID, &s.SourceType, &s.SourceID, &s.Channel, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func listIncidents(ctx context.Context, tenantID, channel, review string, limit int) ([]triageincident.Incident, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	tenantID = strings.TrimSpace(tenantID)
	channel = strings.TrimSpace(channel)
	review = strings.TrimSpace(review)
	q := `
		SELECT id::text FROM ai_triage_incident WHERE 1=1`
	args := []any{}
	n := 1
	if tenantID != "" {
		q += ` AND tenant_id = $` + strconv.Itoa(n) + `::uuid`
		args = append(args, tenantID)
		n++
	}
	if channel != "" {
		q += ` AND channel = $` + strconv.Itoa(n)
		args = append(args, channel)
		n++
	}
	if review != "" {
		q += ` AND review_status = $` + strconv.Itoa(n)
		args = append(args, review)
		n++
	}
	q += ` ORDER BY created_at DESC LIMIT $` + strconv.Itoa(n)
	args = append(args, limit)
	rows, err := system.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	out := make([]triageincident.Incident, 0, len(ids))
	for _, id := range ids {
		inc, err := loadIncident(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, inc)
	}
	return out, nil
}

func updateIncidentReview(ctx context.Context, id, status string, contract json.RawMessage, reviewedBy string) (triageincident.Incident, error) {
	id = strings.TrimSpace(id)
	status = strings.TrimSpace(status)
	switch status {
	case triageincident.ReviewConfirmed, triageincident.ReviewDismissed, triageincident.ReviewNeedsHuman:
	default:
		return triageincident.Incident{}, &errs.Error{Code: errs.InvalidArgument, Message: "status review tidak valid"}
	}
	_, err := system.DB.Exec(ctx, `
		UPDATE ai_triage_incident
		SET review_status = $2,
		    confirmed_contract_json = COALESCE($3::jsonb, confirmed_contract_json),
		    updated_at = now()
		WHERE id = $1::uuid AND review_status IN ('open', 'needs_human_input')`,
		id, status, nullJSON(contract))
	if err != nil {
		return triageincident.Incident{}, err
	}
	_ = reviewedBy
	return loadIncident(ctx, id)
}

func setIncidentBehaviorJob(ctx context.Context, incidentID, jobID string) error {
	_, err := system.DB.Exec(ctx, `
		UPDATE ai_triage_incident SET behavior_job_id = $2::uuid, updated_at = now() WHERE id = $1::uuid`,
		incidentID, jobID)
	return err
}

func setIncidentRepairPlan(ctx context.Context, incidentID, planID string) error {
	_, err := system.DB.Exec(ctx, `
		UPDATE ai_triage_incident SET repair_plan_id = $2::uuid, updated_at = now() WHERE id = $1::uuid`,
		incidentID, planID)
	return err
}

func resolveIncidentSourcesOnly(ctx context.Context, incidentID, jobID, note string) error {
	sources, err := listIncidentSources(ctx, incidentID)
	if err != nil {
		return err
	}
	for _, s := range sources {
		if s.SourceType != triageincident.SourceHumanReport {
			continue
		}
		_, _ = system.DB.Exec(ctx, `
			UPDATE ai_triage_report
			SET status = 'resolved', review_note = $2, resolved_by_job_id = $3::uuid, updated_at = now()
			WHERE id = $1::uuid AND status = 'open'`, s.SourceID, note, jobID)
	}
	_, err = system.DB.Exec(ctx, `
		UPDATE ai_triage_incident
		SET resolution_status = $2, updated_at = now()
		WHERE id = $1::uuid`, incidentID, triageincident.ResolutionFixed)
	return err
}
