package templates

import (
	"context"
	"database/sql"

	appErrs "encore.app/wabantu/shared/errs"
)

type PreviewTokenResponse struct {
	Token     string `json:"token"`
	AssetID   string `json:"assetId"`
	ExpiresIn int    `json:"expiresInSeconds"`
}

//encore:api auth method=POST path=/api/v1/developer/asset-preview-token/:assetId tag:owner
func MintWebComponentPreviewToken(ctx context.Context, assetId string) (*PreviewTokenResponse, error) {
	_, devID, err := developerContext(ctx)
	if err != nil {
		return nil, err
	}
	var kind, status string
	err = sysDB.QueryRow(ctx, `
		SELECT kind, scan_status FROM template_asset
		WHERE id = $1::uuid AND developer_id = $2::uuid`, assetId, devID).
		Scan(&kind, &status)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("asset tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	if kind != "web_component" {
		return nil, appErrs.BadRequest("preview hanya untuk web component")
	}
	if status != "clean" {
		return nil, appErrs.Forbidden("asset belum disetujui")
	}
	tok, err := mintAssetToken(assetSecrets.JWTSecret, assetId)
	if err != nil {
		return nil, err
	}
	return &PreviewTokenResponse{Token: tok, AssetID: assetId, ExpiresIn: 900}, nil
}
