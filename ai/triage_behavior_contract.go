package ai

import (
	"encoding/json"
	"fmt"
	"strings"

	"encore.app/wabantu/shared/triageincident"
)

const behaviorContractVersion = 1

// BehaviorContract is the human-confirmed expected behavior (no code/SQL/regex from LLM).
type BehaviorContract struct {
	Version       int                `json:"version"`
	Lane          string             `json:"lane"`
	Channel       string             `json:"channel"`
	DegradedMode  string             `json:"degradedMode,omitempty"`
	Clarification string             `json:"clarification,omitempty"`
	Assertions    BehaviorAssertions `json:"assertions"`
}

// BehaviorAssertions is a closed, declarative set.
type BehaviorAssertions struct {
	WantPath            string          `json:"wantPath,omitempty"`
	WantStep            string          `json:"wantStep,omitempty"`
	CartInclude         []CartAssertion `json:"cartInclude,omitempty"`
	CartExclude         []string        `json:"cartExclude,omitempty"`
	ReplyContains       []string        `json:"replyContains,omitempty"`
	ReplyExcludes       []string        `json:"replyExcludes,omitempty"`
	RequiredFacts       []string        `json:"requiredFacts,omitempty"`
	ForbiddenClaims     []string        `json:"forbiddenClaims,omitempty"`
	RetrievalKBIDs      []string        `json:"retrievalKbIds,omitempty"`
	RetrievalCatalogIDs []string        `json:"retrievalCatalogIds,omitempty"`
	RetrievalMode       string          `json:"retrievalMode,omitempty"`
	ProductCardIDs      []string        `json:"productCardIds,omitempty"`
	ProductCardSlugs    []string        `json:"productCardSlugs,omitempty"`
	QuickReplies        []string        `json:"quickReplies,omitempty"`
	SearchOrderedIDs    []string        `json:"searchOrderedIds,omitempty"`
	SearchVisibleOnly   bool            `json:"searchVisibleOnly,omitempty"`
	NeedCustomerInput   bool            `json:"needCustomerInput,omitempty"`
}

// CartAssertion is one required cart line.
type CartAssertion struct {
	CatalogItemID string `json:"catalogItemId,omitempty"`
	ExternalCode  string `json:"externalCode,omitempty"`
	NameContains  string `json:"nameContains,omitempty"`
	Qty           int    `json:"qty,omitempty"`
}

// ValidateBehaviorContract rejects code/SQL/regex and unknown lanes.
func ValidateBehaviorContract(c BehaviorContract) error {
	if c.Version != behaviorContractVersion {
		c.Version = behaviorContractVersion
	}
	if !triageincident.ValidLanes[c.Lane] {
		return fmt.Errorf("lane tidak valid")
	}
	if c.Lane == triageincident.LaneMixed {
		return fmt.Errorf("lane mixed wajib dipecah")
	}
	if c.Channel != "" && !map[string]bool{"whatsapp": true, "web_chat": true, "storefront_search": true}[c.Channel] {
		return fmt.Errorf("channel tidak valid")
	}
	if looksLikeCode(c.Clarification) {
		return fmt.Errorf("clarification tidak boleh berisi kode")
	}
	for _, s := range concatStrings(c.Assertions) {
		if looksLikeCode(s) {
			return fmt.Errorf("assertion tidak boleh berisi kode, regex, atau SQL")
		}
	}
	return nil
}

// HasDeterministicInvariant is required before Composer may run.
func HasDeterministicInvariant(c BehaviorContract) bool {
	a := c.Assertions
	if a.NeedCustomerInput {
		return false
	}
	switch c.Lane {
	case triageincident.LaneBuyerflow, triageincident.LaneDraftOrder:
		return a.WantPath != "" || len(a.CartInclude) > 0 || len(a.CartExclude) > 0
	case triageincident.LaneGroundedContent:
		return len(a.RequiredFacts) > 0 || len(a.ForbiddenClaims) > 0 || len(a.ProductCardIDs) > 0
	case triageincident.LaneRetrieval:
		return len(a.RetrievalKBIDs) > 0 || len(a.RetrievalCatalogIDs) > 0
	case triageincident.LaneStorefrontSearch:
		return len(a.SearchOrderedIDs) > 0 || a.SearchVisibleOnly
	default:
		return len(a.ReplyContains) > 0 || len(a.RequiredFacts) > 0
	}
}

func looksLikeCode(s string) bool {
	low := strings.ToLower(s)
	needles := []string{"func ", "package ", "select ", "insert ", "update ", "delete ", "drop ", "alter ", "(?i)", "#!/", "<script"}
	for _, n := range needles {
		if strings.Contains(low, n) {
			return true
		}
	}
	return false
}

func concatStrings(a BehaviorAssertions) []string {
	out := append([]string{}, a.ReplyContains...)
	out = append(out, a.ReplyExcludes...)
	out = append(out, a.RequiredFacts...)
	out = append(out, a.ForbiddenClaims...)
	out = append(out, a.CartExclude...)
	return out
}

// MarshalBehaviorContract stores a validated contract.
func MarshalBehaviorContract(c BehaviorContract) (json.RawMessage, error) {
	if err := ValidateBehaviorContract(c); err != nil {
		return nil, err
	}
	c.Version = behaviorContractVersion
	return json.Marshal(c)
}
