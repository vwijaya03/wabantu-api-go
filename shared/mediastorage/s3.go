package mediastorage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"

	"encore.dev/rlog"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var secrets struct {
	AWSS3Bucket          string
	AWSS3Region          string
	AWSS3AccessKeyID     string
	AWSS3SecretAccessKey string
}

var (
	clientOnce sync.Once
	s3Client   *s3.Client
	clientErr  error
)

// s3Configured reports whether all required S3 settings are non-empty.
func s3Configured(bucket, region, accessKeyID, secretAccessKey string) bool {
	return strings.TrimSpace(bucket) != "" &&
		strings.TrimSpace(region) != "" &&
		strings.TrimSpace(accessKeyID) != "" &&
		strings.TrimSpace(secretAccessKey) != ""
}

// Configured reports whether S3 credentials and bucket are available.
func Configured() bool {
	return s3Configured(
		secrets.AWSS3Bucket,
		secrets.AWSS3Region,
		secrets.AWSS3AccessKeyID,
		secrets.AWSS3SecretAccessKey,
	)
}

func getClient(ctx context.Context) (*s3.Client, error) {
	if !Configured() {
		return nil, fmt.Errorf("s3 not configured")
	}
	clientOnce.Do(func() {
		cfg, err := config.LoadDefaultConfig(ctx,
			config.WithRegion(strings.TrimSpace(secrets.AWSS3Region)),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				strings.TrimSpace(secrets.AWSS3AccessKeyID),
				strings.TrimSpace(secrets.AWSS3SecretAccessKey),
				"",
			)),
		)
		if err != nil {
			clientErr = fmt.Errorf("load aws config: %w", err)
			return
		}
		// AWS native S3 only (no custom endpoint secret — Encore requires all secrets set per env).
		s3Client = s3.NewFromConfig(cfg)
	})
	if clientErr != nil {
		return nil, clientErr
	}
	return s3Client, nil
}

// BuildInboxMediaKey returns the object key for an inbox media file.
// Format: {tenantSchema}/inbox/{messageID}/{sha256_prefix}.{ext}
func BuildInboxMediaKey(tenantSchema, messageID string, data []byte, mime string) string {
	sum := sha256.Sum256(data)
	prefix := hex.EncodeToString(sum[:])[:16]
	return fmt.Sprintf("%s/inbox/%s/%s.%s",
		strings.TrimSpace(tenantSchema),
		strings.TrimSpace(messageID),
		prefix,
		extFromMIME(mime),
	)
}

// Put stores bytes in the configured S3 bucket.
func Put(ctx context.Context, key string, data []byte, contentType string) error {
	client, err := getClient(ctx)
	if err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("empty s3 key")
	}
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(strings.TrimSpace(secrets.AWSS3Bucket)),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("s3 put object: %w", err)
	}
	return nil
}

// Get reads an object from the configured S3 bucket.
func Get(ctx context.Context, key string) ([]byte, string, error) {
	client, err := getClient(ctx)
	if err != nil {
		return nil, "", err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, "", fmt.Errorf("empty s3 key")
	}
	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(strings.TrimSpace(secrets.AWSS3Bucket)),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", fmt.Errorf("s3 get object: %w", err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(io.LimitReader(out.Body, maxObjectBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read s3 body: %w", err)
	}
	if len(data) > maxObjectBytes {
		return nil, "", fmt.Errorf("s3 object exceeds max size")
	}
	mime := "application/octet-stream"
	if out.ContentType != nil {
		mime = strings.TrimSpace(*out.ContentType)
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	return data, mime, nil
}

// Delete removes an object from the configured S3 bucket.
func Delete(ctx context.Context, key string) error {
	client, err := getClient(ctx)
	if err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("empty s3 key")
	}
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(strings.TrimSpace(secrets.AWSS3Bucket)),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("s3 delete object: %w", err)
	}
	return nil
}

const maxObjectBytes = 10 << 20 // 10 MiB, aligned with WhatsApp download limit

func extFromMIME(mime string) string {
	mime = strings.ToLower(strings.TrimSpace(strings.Split(mime, ";")[0]))
	switch mime {
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	case "video/mp4":
		return "mp4"
	case "video/3gpp":
		return "3gp"
	case "audio/ogg":
		return "ogg"
	case "audio/mpeg":
		return "mp3"
	case "audio/aac":
		return "aac"
	case "application/pdf":
		return "pdf"
	default:
		if strings.HasPrefix(mime, "image/") {
			return "img"
		}
		rlog.Debug("mediastorage: unknown mime, using bin", "mime", mime)
		return "bin"
	}
}
