package tenant

import (
	"reflect"
	"testing"
)

func TestDiffOrphanSchemas(t *testing.T) {
	all := []string{"t_a", "t_b", "t_orphan"}
	registered := []string{"t_a", "t_b"}
	got := diffOrphanSchemas(all, registered)
	want := []string{"t_orphan"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diffOrphanSchemas() = %v, want %v", got, want)
	}
}

func TestDiffOrphanSchemas_None(t *testing.T) {
	all := []string{"t_a"}
	got := diffOrphanSchemas(all, []string{"t_a"})
	if len(got) != 0 {
		t.Fatalf("expected no orphans, got %v", got)
	}
}
