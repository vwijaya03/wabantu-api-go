package kbcontext

import "context"

// QueriedContext is the input for a future chatengine/storefront caller.
// WA autoreply continues to use ai/retrieval_bridge until that extract is complete;
// this type is the stable contract so chatengine must not copy loaders.
type Query struct {
	TenantID     string
	TenantSchema string
	Text         string
}

// Provider is implemented by production retrieval (ai.AutoReplyService adapter later).
type Provider interface {
	Retrieve(ctx context.Context, q Query) (Bundle, error)
}
