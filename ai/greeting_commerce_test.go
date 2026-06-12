package ai

import "testing"

func TestIsGreetingLikeFormalPrefixNotCommerce(t *testing.T) {
	if IsGreetingLike("Selamat siang kak, jualan apa aja") {
		t.Fatal("browse catalog with formal prefix must not be greeting-only")
	}
	if !IsGreetingLike("Selamat siang kak, halo") {
		t.Fatal("formal prefix + halo should stay greeting")
	}
}

func TestIsGreetingLikeRegionalLeadIn(t *testing.T) {
	if !IsGreetingLike("monggo mas, permisi") {
		t.Fatal("regional lead-in + permisi should be greeting")
	}
	if IsGreetingLike("monggo mas, cari abon sapi") {
		t.Fatal("regional lead-in + product search must not be greeting")
	}
}
