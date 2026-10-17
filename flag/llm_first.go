package flag

import "context"

// Flags of the per-tenant LLM-first reply pipeline in the ai package. Every flag defaults to off,
// so a tenant without any of them behaves exactly as before.
const (
	FlagLLMFirst       = "ai_llm_first"        // enabled for the tenant: the pipeline answers its buyers
	FlagLLMFirstShadow = "ai_llm_first_shadow" // enabled for the tenant: the pipeline runs and logs, never sends
	FlagLLMFirstKill   = "ai_llm_first_kill"   // enabled_globally=true switches the pipeline off for everyone
)

// LLMFirstMode resolves the pipeline mode for a tenant: "off", "shadow" or "on".
// Order of checks: the kill flag first, then ai_llm_first, then ai_llm_first_shadow; with none of
// them enabled the mode is "off".
func LLMFirstMode(ctx context.Context, tenantID string) string {
	if IsEnabled(ctx, FlagLLMFirstKill, tenantID) {
		return "off"
	}
	if IsEnabled(ctx, FlagLLMFirst, tenantID) {
		return "on"
	}
	if IsEnabled(ctx, FlagLLMFirstShadow, tenantID) {
		return "shadow"
	}
	return "off"
}
