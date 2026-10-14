package inbox

import "context"

// CacheMessageMedia stores media bytes for an inbound message in the inbox media cache so
// FetchMessageMediaBytes (payment-proof OCR, image context) can read it without Meta.
// Used by the judge simulator. TTL = inboxMediaCacheTTL (1h).
func CacheMessageMedia(ctx context.Context, tenantSchema, messageID, mime string, data []byte) {
	storeCachedInboxMedia(ctx, tenantSchema, messageID, mime, data)
}
