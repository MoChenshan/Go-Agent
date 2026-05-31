package sessionuid

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestAllocateDeterministic(t *testing.T) {
	tests := []string{
		"ws_session-1779348859",
		"ws_6bee10fd-2959-4803-9abc-221274317855",
		"ws_doc_d81e2cqkh3hdcdkgebpg_ba8ca659-87ba-4f27-9186-4d50cf6f4024",
		"ws_mpdvkqi3exve5dp5qqn",
	}
	for _, id := range tests {
		first := Allocate(id)
		for range 5 {
			if got := Allocate(id); got != first {
				t.Fatalf("Allocate(%q) not deterministic: got %d, want %d", id, got, first)
			}
		}
	}
}

func TestAllocateInRange(t *testing.T) {
	for i := range 1000 {
		uid := Allocate(fmt.Sprintf("ws_session-%d", i))
		if uid < MinUID || uid >= MinUID+RangeSize {
			t.Fatalf("Allocate returned %d, out of [%d, %d)", uid, MinUID, MinUID+RangeSize)
		}
	}
}

func TestAllocateCollisionRateReasonable(t *testing.T) {
	// Sample 1000 random workspace IDs. Expected pairwise collisions
	// ≈ 1000 × 999 / (2 × RangeSize) ≈ 2.5 × 10⁻⁴ — effectively zero.
	// Allow up to 5 to absorb rare birthday-like surprises without
	// flaking on a future seed change.
	const samples = 1000
	r := rand.New(rand.NewSource(1))
	seen := map[uint32]int{}
	for range samples {
		id := fmt.Sprintf("ws_exec-%016x", r.Uint64())
		seen[Allocate(id)]++
	}
	collisions := 0
	for _, c := range seen {
		if c > 1 {
			collisions += c - 1
		}
	}
	if collisions > 5 {
		t.Fatalf("Too many collisions: %d out of %d (expected ~0)", collisions, samples)
	}
}

func TestAllocateEmptyID(t *testing.T) {
	if got := Allocate(""); got != MinUID {
		t.Fatalf("Allocate(\"\") = %d, want %d", got, MinUID)
	}
}

func TestAllocateDifferentIDsLikelyDifferentUIDs(t *testing.T) {
	a := Allocate("ws_session-A")
	b := Allocate("ws_session-B")
	if a == b {
		t.Fatalf("Allocate produced the same uid %d for two distinct IDs; "+
			"unlikely but acceptable — re-evaluate test inputs", a)
	}
}

func TestUsernameMatchesAllocate(t *testing.T) {
	for _, id := range []string{
		"ws_session-1779348859",
		"ws_6bee10fd-2959-4803-9abc-221274317855",
	} {
		uid := Allocate(id)
		want := fmt.Sprintf("pcg123_%d", uid)
		if got := Username(id); got != want {
			t.Fatalf("Username(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestUsernameDeterministic(t *testing.T) {
	id := "ws_stable"
	first := Username(id)
	for range 5 {
		if got := Username(id); got != first {
			t.Fatalf("Username(%q) not deterministic: got %q, want %q", id, got, first)
		}
	}
}
