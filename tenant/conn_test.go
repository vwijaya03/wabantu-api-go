package tenant

import (
	"testing"
)

func TestValidateTenantSchemaName(t *testing.T) {
	tests := []struct {
		schema string
		ok     bool
	}{
		{"t_acme", true},
		{"public", false},
		{"", false},
		{"t-foo", false},
	}
	for _, tc := range tests {
		err := ValidateTenantSchemaName(tc.schema)
		if tc.ok && err != nil {
			t.Fatalf("schema %q: want ok, got %v", tc.schema, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("schema %q: want error", tc.schema)
		}
	}
}
