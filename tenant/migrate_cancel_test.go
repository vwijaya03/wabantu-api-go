package tenant

import (
	"context"
	"testing"
)

func TestCancelSchemaMigrationJobRequiresJobID(t *testing.T) {
	_, err := CancelSchemaMigrationJob(context.Background(), "")
	if err == nil || err.Error() != "jobId required" {
		t.Fatalf("CancelSchemaMigrationJob() err = %v, want jobId required", err)
	}
}

func TestCancelSchemaMigrationJobRequiresJobIDWhitespace(t *testing.T) {
	_, err := CancelSchemaMigrationJob(context.Background(), "   ")
	if err == nil || err.Error() != "jobId required" {
		t.Fatalf("CancelSchemaMigrationJob() err = %v, want jobId required", err)
	}
}
