package events

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
)

type contactPatientFields struct {
	ContactID string
	FullName  string
	BirthDate string
	Phone     string
}

func resolvePatientContact(ctx context.Context, ts tenantScope, p *CreatePatientParams) (*contactPatientFields, error) {
	if p == nil {
		return nil, appErrs.BadRequest("data pasien wajib diisi")
	}
	contactID := strings.TrimSpace(p.ContactID)
	if contactID == "" {
		return &contactPatientFields{
			FullName:  strings.TrimSpace(p.FullName),
			BirthDate: strings.TrimSpace(p.BirthDate),
		}, nil
	}

	var displayName, phone, notes sql.NullString
	var birthDate sql.NullString
	err := ts.QueryRowContext(ctx, `
		SELECT display_name, phone_number, birth_date::text, notes
		FROM contact
		WHERE id=$1::uuid AND deleted_at IS NULL`, contactID,
	).Scan(&displayName, &phone, &birthDate, &notes)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("kontak tidak ditemukan")
	}
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}

	fullName := strings.TrimSpace(p.FullName)
	if fullName == "" && displayName.Valid {
		fullName = strings.TrimSpace(displayName.String)
	}
	if fullName == "" && phone.Valid {
		fullName = strings.TrimSpace(phone.String)
	}
	if fullName == "" {
		return nil, appErrs.BadRequest("kontak tidak punya nama — isi nama di kontak atau ketik manual")
	}

	bd := strings.TrimSpace(p.BirthDate)
	if bd == "" && birthDate.Valid {
		bd = strings.TrimSpace(birthDate.String)
		if len(bd) > 10 {
			bd = bd[:10]
		}
	}
	if bd == "" {
		return nil, appErrs.BadRequest(fmt.Sprintf(
			"tanggal lahir wajib — lengkapi di menu Contacts untuk %s", fullName))
	}

	complaint := strings.TrimSpace(p.Complaint)
	if complaint == "" && notes.Valid {
		complaint = strings.TrimSpace(notes.String)
	}
	p.Complaint = complaint
	p.FullName = fullName
	p.BirthDate = bd

	return &contactPatientFields{
		ContactID: contactID,
		FullName:  fullName,
		BirthDate: bd,
		Phone:     phone.String,
	}, nil
}
