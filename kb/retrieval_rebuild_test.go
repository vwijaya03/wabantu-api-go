package kb

import "testing"

func TestEntityTypesForRebuild(t *testing.T) {
	got := entityTypesForRebuild(true, true)
	if len(got) != 2 || got[0] != entityTypeKB || got[1] != catalogEntityType {
		t.Fatalf("both: got %v", got)
	}
	got = entityTypesForRebuild(true, false)
	if len(got) != 1 || got[0] != entityTypeKB {
		t.Fatalf("kb only: got %v", got)
	}
	got = entityTypesForRebuild(false, true)
	if len(got) != 1 || got[0] != catalogEntityType {
		t.Fatalf("catalog only: got %v", got)
	}
	if len(entityTypesForRebuild(false, false)) != 0 {
		t.Fatal("expected empty when neither included")
	}
}
