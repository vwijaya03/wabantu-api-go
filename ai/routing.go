package ai

import (
	"strings"
)

// Anthropic model ids (override per-request; not tied to a single .env model).
const (
	ModelHaiku  = "claude-3-5-haiku-20241022"
	ModelSonnet = "claude-sonnet-4-5-20250514"
)

// PlanAIRouting describes which routing strategy a subscription plan uses.
type PlanAIRouting string

const (
	PlanRoutingHaikuOnly       PlanAIRouting = "haiku_only"        // STARTER
	PlanRoutingHybrid          PlanAIRouting = "hybrid"            // BUSINESS
	PlanRoutingHybridPriority  PlanAIRouting = "hybrid_priority"   // PRO
)

// MessageComplexity drives Haiku vs Sonnet within hybrid plans.
type MessageComplexity string

const (
	ComplexitySimple  MessageComplexity = "simple"
	ComplexityComplex MessageComplexity = "complex"
)

// RoutingDecision is the resolved model for one LLM call.
type RoutingDecision struct {
	Model      string            `json:"model"`
	Tier       string            `json:"tier"` // haiku | sonnet
	Complexity MessageComplexity `json:"complexity"`
	PlanMode   PlanAIRouting     `json:"planMode"`
	Reason     string            `json:"reason"`
}

// PlanRoutingMode maps subscription plan_code to AI routing strategy.
func PlanRoutingMode(planCode string) PlanAIRouting {
	switch strings.ToLower(strings.TrimSpace(planCode)) {
	case "starter", "trial":
		return PlanRoutingHaikuOnly
	case "business", "basic":
		return PlanRoutingHybrid
	case "pro", "enterprise":
		return PlanRoutingHybridPriority
	default:
		return PlanRoutingHaikuOnly
	}
}

// ResolveRouting picks Haiku or Sonnet from plan + message complexity.
func ResolveRouting(planCode string, complexity MessageComplexity) RoutingDecision {
	mode := PlanRoutingMode(planCode)

	switch mode {
	case PlanRoutingHaikuOnly:
		return RoutingDecision{
			Model: ModelHaiku, Tier: "haiku", Complexity: complexity,
			PlanMode: mode, Reason: "plan_starter_haiku_only",
		}
	case PlanRoutingHybridPriority:
		if complexity == ComplexitySimple {
			return RoutingDecision{
				Model: ModelHaiku, Tier: "haiku", Complexity: complexity,
				PlanMode: mode, Reason: "pro_simple_faq",
			}
		}
		return RoutingDecision{
			Model: ModelSonnet, Tier: "sonnet", Complexity: complexity,
			PlanMode: mode, Reason: "pro_priority_sonnet",
		}
	default: // hybrid (business)
		if complexity == ComplexitySimple {
			return RoutingDecision{
				Model: ModelHaiku, Tier: "haiku", Complexity: complexity,
				PlanMode: mode, Reason: "hybrid_simple",
			}
		}
		return RoutingDecision{
			Model: ModelSonnet, Tier: "sonnet", Complexity: complexity,
			PlanMode: mode, Reason: "hybrid_complex",
		}
	}
}
