package storefront

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net"
	"regexp"
	"strings"
	"time"

	"encore.dev/beta/auth"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/types"
)

var hostnameRe = regexp.MustCompile(`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(?:\.(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?))+$`)

type CustomDomain struct {
	ID                 string     `json:"id"`
	Hostname           string     `json:"hostname"`
	Status             string     `json:"status"`
	VerificationToken  string     `json:"verificationToken,omitempty"`
	CNAMETarget        string     `json:"cnameTarget"`
	VerifiedAt         *time.Time `json:"verifiedAt,omitempty"`
	DNSRecordName      string     `json:"dnsRecordName"`
	DNSRecordValue     string     `json:"dnsRecordValue"`
}

type AddCustomDomainRequest struct {
	Hostname string `json:"hostname"`
}

type ListCustomDomainsResponse struct {
	Items []CustomDomain `json:"items"`
}

//encore:api auth method=GET path=/api/v1/storefront/custom-domains tag:owner
func ListCustomDomains(ctx context.Context) (*ListCustomDomainsResponse, error) {
	u, err := ownerUser(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := systemDB.Query(ctx, `
		SELECT id::text, hostname, status, verification_token, cname_target, verified_at
		FROM tenant_custom_domain WHERE tenant_id = $1::uuid ORDER BY created_at DESC`, u.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []CustomDomain
	for rows.Next() {
		var d CustomDomain
		if err := rows.Scan(&d.ID, &d.Hostname, &d.Status, &d.VerificationToken, &d.CNAMETarget, &d.VerifiedAt); err != nil {
			return nil, err
		}
		d.DNSRecordName = "_wabantu-verify." + d.Hostname
		d.DNSRecordValue = d.VerificationToken
		items = append(items, d)
	}
	return &ListCustomDomainsResponse{Items: items}, rows.Err()
}

//encore:api auth method=POST path=/api/v1/storefront/custom-domains tag:owner
func AddCustomDomain(ctx context.Context, req *AddCustomDomainRequest) (*CustomDomain, error) {
	u, err := ownerUser(ctx)
	if err != nil {
		return nil, err
	}
	host := normalizeHostname(req.Hostname)
	if err := validateHostname(host); err != nil {
		return nil, err
	}
	tok, err := mintVerificationToken()
	if err != nil {
		return nil, err
	}
	var d CustomDomain
	err = systemDB.QueryRow(ctx, `
		INSERT INTO tenant_custom_domain (tenant_id, hostname, verification_token, status, cname_target)
		VALUES ($1::uuid, $2, $3, 'pending', 'custom.wabantu.id')
		RETURNING id::text, hostname, status, verification_token, cname_target`,
		u.TenantID, host, tok).
		Scan(&d.ID, &d.Hostname, &d.Status, &d.VerificationToken, &d.CNAMETarget)
	if err != nil {
		return nil, err
	}
	d.DNSRecordName = "_wabantu-verify." + d.Hostname
	d.DNSRecordValue = d.VerificationToken
	return &d, nil
}

//encore:api auth method=POST path=/api/v1/storefront/custom-domains/:id/verify tag:owner
func VerifyCustomDomain(ctx context.Context, id string) (*CustomDomain, error) {
	u, err := ownerUser(ctx)
	if err != nil {
		return nil, err
	}
	var d CustomDomain
	err = systemDB.QueryRow(ctx, `
		SELECT id::text, hostname, status, verification_token, cname_target, verified_at
		FROM tenant_custom_domain WHERE id = $1::uuid AND tenant_id = $2::uuid`,
		id, u.TenantID).
		Scan(&d.ID, &d.Hostname, &d.Status, &d.VerificationToken, &d.CNAMETarget, &d.VerifiedAt)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("domain tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	if d.Status == "verified" {
		return &d, nil
	}
	record := "_wabantu-verify." + d.Hostname
	txts, err := net.LookupTXT(record)
	if err != nil {
		return nil, appErrs.BadRequest("DNS TXT belum ditemukan — tambahkan record verifikasi")
	}
	found := false
	for _, txt := range txts {
		if strings.TrimSpace(txt) == d.VerificationToken {
			found = true
			break
		}
	}
	if !found {
		return nil, appErrs.BadRequest("token verifikasi DNS tidak cocok")
	}
	var verifiedAt time.Time
	err = systemDB.QueryRow(ctx, `
		UPDATE tenant_custom_domain SET status = 'verified', verified_at = now(), updated_at = now()
		WHERE id = $1::uuid RETURNING verified_at`, id).Scan(&verifiedAt)
	if err != nil {
		return nil, err
	}
	d.Status = "verified"
	d.VerifiedAt = &verifiedAt
	return &d, nil
}

func ownerUser(ctx context.Context) (*types.AuthUser, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, appErrs.Unauthenticated("not authenticated")
	}
	if !u.CanPerformOwnerActions() {
		return nil, appErrs.Forbidden("owner access required")
	}
	return u, nil
}

func normalizeHostname(h string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
}

func validateHostname(host string) error {
	if host == "" || len(host) > 253 {
		return appErrs.BadRequest("hostname tidak valid")
	}
	if strings.Contains(host, "*") || strings.Contains(host, "localhost") {
		return appErrs.BadRequest("hostname tidak diizinkan")
	}
	if net.ParseIP(host) != nil {
		return appErrs.BadRequest("hostname tidak boleh berupa IP")
	}
	if strings.HasSuffix(host, ".wabantu.id") || host == "wabantu.id" {
		return appErrs.BadRequest("gunakan subdomain bawaan untuk domain platform")
	}
	if !hostnameRe.MatchString(host) {
		return appErrs.BadRequest("format hostname tidak valid")
	}
	return nil
}

func mintVerificationToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "wabantu-" + hex.EncodeToString(b), nil
}
