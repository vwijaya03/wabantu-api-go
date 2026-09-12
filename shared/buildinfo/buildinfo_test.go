package buildinfo

import "testing"

func TestCoversRejectsUnknown(t *testing.T) {
	if Covers(RevisionUnknown, "abc1234") {
		t.Fatal("unknown deploy must not cover")
	}
	if Covers("", "abc1234") {
		t.Fatal("empty deploy must not cover")
	}
	if !Covers("abcdef0deadbeef", "abcdef0") {
		t.Fatal("prefix should cover")
	}
	if Covers("1111111", "2222222") {
		t.Fatal("different SHAs must not cover")
	}
}
