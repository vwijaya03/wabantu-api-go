package adapters

import (
	"testing"

	"encore.app/wabantu/shared/interactionevidence"
)

func TestFromIngestChannelRouting(t *testing.T) {
	wa := FromIngest(IngestInput{Channel: "whatsapp", ConversationID: "c1", InboundID: "i1"})
	if wa.Channel != interactionevidence.ChannelWhatsApp {
		t.Fatalf("wa channel=%s", wa.Channel)
	}
	web := FromIngest(IngestInput{Channel: "web_chat", ConversationID: "sess", InboundID: "i2", UserText: "hi"})
	if web.Channel != interactionevidence.ChannelWebChat || web.ThreadRef != "sess" {
		t.Fatalf("web=%+v", web)
	}
	search := FromIngest(IngestInput{Channel: "storefront_search", ConversationID: "t1", UserText: "oatlife"})
	if search.Channel != interactionevidence.ChannelStorefrontSearch || search.UserText != "oatlife" {
		t.Fatalf("search=%+v", search)
	}
}
