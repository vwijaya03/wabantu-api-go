package tenant

import "testing"

func TestMigrationBusyMaxAttemptsPositive(t *testing.T) {
	if migrationBusyMaxAttempts < 3 {
		t.Fatalf("migrationBusyMaxAttempts too low: %d", migrationBusyMaxAttempts)
	}
}

func TestMigrationStaleRunningIntervalSet(t *testing.T) {
	if migrationStaleRunningInterval == "" {
		t.Fatal("migrationStaleRunningInterval must be set")
	}
}
