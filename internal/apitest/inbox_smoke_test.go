package apitest_test

import (
	"context"
	"testing"

	"encore.app/wabantu/inbox"
	"encore.app/wabantu/internal/apitest"
)

func TestInboxListConversationsSmoke(t *testing.T) {
	fx := apitest.BootstrapOwner(t)
	apitest.WithOwnerAuth(fx)

	resp, err := inbox.ListConversations(context.Background(), &inbox.ListConversationsParams{Limit: 10})
	if err != nil {
		t.Fatalf("GET /api/v1/inbox/conversations: %v", err)
	}
	if resp == nil {
		t.Fatal("nil ListConversationsResponse")
	}
	if resp.Items == nil {
		t.Fatal("items slice is nil")
	}
	if len(resp.Items) != 0 {
		t.Fatalf("expected empty conversations, got %d", len(resp.Items))
	}
}
