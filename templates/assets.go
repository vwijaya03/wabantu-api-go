package templates

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"strings"

	"encore.dev/beta/auth"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/shared/mediastorage"
	"encore.app/wabantu/shared/templateassets"
	"encore.app/wabantu/shared/types"
)

var assetSecrets struct {
	JWTSecret string
}

type UploadLottieRequest struct {
	Filename string `json:"filename"`
	Content  string `json:"contentBase64"`
}

type UploadAssetResponse struct {
	AssetID string `json:"assetId"`
	Kind    string `json:"kind"`
}

type AssetSignedURLResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expiresInSeconds"`
}

//encore:api auth method=POST path=/api/v1/developer/lottie-assets tag:owner
func UploadLottieAsset(ctx context.Context, req *UploadLottieRequest) (*UploadAssetResponse, error) {
	_, devID, err := developerContext(ctx)
	if err != nil {
		return nil, err
	}
	if !mediastorage.Configured() {
		return nil, appErrs.Unavailable("penyimpanan asset belum dikonfigurasi")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.Content))
	if err != nil {
		return nil, appErrs.BadRequest("contentBase64 tidak valid")
	}
	if err := templateassets.ValidateLottieJSON(raw); err != nil {
		return nil, appErrs.BadRequest(err.Error())
	}
	return persistAsset(ctx, devID, "lottie", raw, "json", "application/json")
}

type UploadWebComponentRequest struct {
	Filename string `json:"filename"`
	Content  string `json:"contentBase64"`
}

//encore:api auth method=POST path=/api/v1/developer/web-component-assets tag:owner
func UploadWebComponentAsset(ctx context.Context, req *UploadWebComponentRequest) (*UploadAssetResponse, error) {
	_, devID, err := developerContext(ctx)
	if err != nil {
		return nil, err
	}
	if !mediastorage.Configured() {
		return nil, appErrs.Unavailable("penyimpanan asset belum dikonfigurasi")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.Content))
	if err != nil {
		return nil, appErrs.BadRequest("contentBase64 tidak valid")
	}
	if err := templateassets.ValidateWebComponentSource(raw); err != nil {
		return nil, appErrs.BadRequest(err.Error())
	}
	return persistAsset(ctx, devID, "web_component", raw, "js", "text/javascript")
}

//encore:api auth method=POST path=/api/v1/developer/asset-signed-url/:assetId tag:owner
func MintAssetSignedURL(ctx context.Context, assetId string) (*AssetSignedURLResponse, error) {
	_, devID, err := developerContext(ctx)
	if err != nil {
		return nil, err
	}
	var ownerDevID, status string
	err = sysDB.QueryRow(ctx, `
		SELECT developer_id::text, scan_status FROM template_asset WHERE id = $1::uuid`, assetId).
		Scan(&ownerDevID, &status)
	if err == sql.ErrNoRows {
		return nil, appErrs.NotFound("asset tidak ditemukan")
	}
	if err != nil {
		return nil, err
	}
	if ownerDevID != devID {
		return nil, appErrs.Forbidden("bukan asset Anda")
	}
	if status != "clean" {
		return nil, appErrs.Forbidden("asset belum disetujui")
	}
	tok, err := mintAssetToken(assetSecrets.JWTSecret, assetId)
	if err != nil {
		return nil, err
	}
	return &AssetSignedURLResponse{Token: tok, ExpiresIn: 900}, nil
}

func developerContext(ctx context.Context) (*types.AuthUser, string, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, "", appErrs.Unauthenticated("not authenticated")
	}
	var devID string
	err := sysDB.QueryRow(ctx, `SELECT id::text FROM developer_account WHERE tenant_id = $1::uuid`, u.TenantID).Scan(&devID)
	if err == sql.ErrNoRows {
		return nil, "", appErrs.Forbidden("daftar sebagai developer terlebih dahulu")
	}
	if err != nil {
		return nil, "", err
	}
	return u, devID, nil
}

func persistAsset(ctx context.Context, devID, kind string, raw []byte, ext, mime string) (*UploadAssetResponse, error) {
	key := mediastorage.BuildTemplateAssetKey(devID, raw, ext)
	if err := mediastorage.Put(ctx, key, raw, mime); err != nil {
		return nil, appErrs.Unavailable("gagal menyimpan asset")
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	var assetID string
	err := sysDB.QueryRow(ctx, `
		INSERT INTO template_asset (developer_id, kind, content_sha256, s3_key, byte_size, scan_status)
		VALUES ($1::uuid, $2, $3, $4, $5, 'clean')
		ON CONFLICT (s3_key) DO UPDATE SET scan_status = 'clean'
		RETURNING id::text`,
		devID, kind, hash, key, len(raw)).Scan(&assetID)
	if err != nil {
		return nil, err
	}
	return &UploadAssetResponse{AssetID: assetID, Kind: kind}, nil
}
