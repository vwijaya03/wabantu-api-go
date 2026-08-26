package apitest

import (
	"context"
	"fmt"
	"testing"

	"encore.app/wabantu/tenant"
	"encore.app/wabantu/workflow"
)

const workflowRuleDDL = `
CREATE TABLE IF NOT EXISTS workflow_rule (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    trigger_type   VARCHAR(40) NOT NULL DEFAULT 'message_contains',
    trigger_value  TEXT NOT NULL,
    action_type    VARCHAR(40) NOT NULL DEFAULT 'send_reply',
    action_payload JSONB NOT NULL DEFAULT '{}',
    branch_id      UUID,
    is_active      BOOLEAN NOT NULL DEFAULT true,
    priority       INT NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at     TIMESTAMPTZ,
    deleted_by     UUID
);`

// ensureWorkflowRuleTable creates workflow_rule without full RunSchemaPatches (pg_trgm unavailable in Encore test cluster).
func ensureWorkflowRuleTable(t *testing.T, schemaName string) {
	t.Helper()
	ctx := context.Background()
	_, err := tenant.DataDB.Stdlib().ExecContext(ctx,
		fmt.Sprintf("SET search_path TO %q; %s", schemaName, workflowRuleDDL))
	if err != nil {
		t.Fatalf("ensure workflow_rule(%s): %v", schemaName, err)
	}
}

func TestWorkflowSmoke_ListRules(t *testing.T) {
	RequireEncoreInfra(t)
	fx := BootstrapOwner(t)
	ensureWorkflowRuleTable(t, fx.SchemaName)
	WithOwnerAuth(fx)

	ctx := context.Background()
	out, err := workflow.ListRules(ctx)
	if err != nil {
		t.Fatalf("workflow.ListRules: %v", err)
	}
	AssertJSONArrayField(t, out, "rules")
}
