package templates

import (
	"context"
	"time"

	appErrs "encore.app/wabantu/shared/errs"
)

type TemplateReviewItem struct {
	VersionID   string    `json:"versionId"`
	Slug        string    `json:"slug"`
	Version     string    `json:"version"`
	SubmittedAt time.Time `json:"submittedAt"`
}

type ListTemplateReviewsResponse struct {
	Items []TemplateReviewItem `json:"items"`
}

type DecideReviewRequest struct {
	Decision string `json:"decision"`
	Notes    string `json:"notes,omitempty"`
}

//encore:api auth method=GET path=/api/v1/admin/template-reviews tag:super_admin
func ListTemplateReviews(ctx context.Context) (*ListTemplateReviewsResponse, error) {
	rows, err := sysDB.Query(ctx, `
		SELECT tv.id::text, tl.slug, tv.version, tv.created_at
		FROM template_version tv
		JOIN template_listing tl ON tl.id = tv.listing_id
		WHERE tv.status = 'in_review'
		ORDER BY tv.created_at ASC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []TemplateReviewItem
	for rows.Next() {
		var it TemplateReviewItem
		if err := rows.Scan(&it.VersionID, &it.Slug, &it.Version, &it.SubmittedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return &ListTemplateReviewsResponse{Items: items}, rows.Err()
}

//encore:api auth method=POST path=/api/v1/admin/template-reviews/:versionId/decide tag:super_admin
func DecideTemplateReview(ctx context.Context, versionId string, req *DecideReviewRequest) error {
	switch req.Decision {
	case "approved", "rejected", "changes_requested":
	default:
		return appErrs.BadRequest("decision tidak valid")
	}
	status := "draft"
	if req.Decision == "approved" {
		status = "published"
	}
	_, err := sysDB.Exec(ctx, `
		UPDATE template_version SET status = $2 WHERE id = $1::uuid AND status = 'in_review'`,
		versionId, status)
	return err
}
