package entitlement

import (
	"context"

	appErrs "encore.app/wabantu/shared/errs"
	"encore.app/wabantu/usage"
)

// Feature keys used for plan gating (trial bypasses map — all true; see HasFeature).
const (
	FeatureCRMLeads    = "crm_leads"
	FeatureBroadcast   = "broadcast"
	FeatureHybridAI    = "hybrid_ai"
	FeatureAPIAccess   = "api_access"
	FeatureWorkflow    = "workflow"
	FeatureMultiBranch = "multi_branch"
)

// Trial: all product surfaces enabled; tight caps live in usage.planQuotas["trial"].
var planFeatures = map[string]map[string]bool{
	"starter": {
		FeatureCRMLeads:    false,
		FeatureBroadcast:   false,
		FeatureHybridAI:    false,
		FeatureAPIAccess:   false,
		FeatureWorkflow:    false,
		FeatureMultiBranch: false,
	},
	"business": {
		FeatureCRMLeads:    true,
		FeatureBroadcast:   true,
		FeatureHybridAI:    true,
		FeatureAPIAccess:   false,
		FeatureWorkflow:    true,
		FeatureMultiBranch: false,
	},
	"basic": { // legacy alias
		FeatureCRMLeads:    true,
		FeatureBroadcast:   true,
		FeatureHybridAI:    true,
		FeatureAPIAccess:   false,
		FeatureWorkflow:    true,
		FeatureMultiBranch: false,
	},
	"pro": {
		FeatureCRMLeads:    true,
		FeatureBroadcast:   true,
		FeatureHybridAI:    true,
		FeatureAPIAccess:   true,
		FeatureWorkflow:    true,
		FeatureMultiBranch: true,
	},
}

// HasFeature returns whether planCode includes the feature.
func HasFeature(planCode, feature string) bool {
	if planCode == "trial" {
		return true
	}
	if planCode == "basic" {
		planCode = "business"
	}
	feats, ok := planFeatures[planCode]
	if !ok {
		feats = planFeatures["starter"]
	}
	return feats[feature]
}

// Require returns Forbidden if the tenant plan does not include the feature.
func Require(ctx context.Context, tenantSchema, feature string) error {
	plan := usage.TenantPlan(ctx, tenantSchema)
	if !HasFeature(plan, feature) {
		return appErrs.Forbidden("feature not available on your plan")
	}
	return nil
}
