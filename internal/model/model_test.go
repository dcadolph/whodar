package model

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// TestNewGraph verifies the constructor returns usable, non-nil maps.
func TestNewGraph(t *testing.T) {
	t.Parallel()
	g := NewGraph()
	if g.People == nil || g.Teams == nil || g.Orgs == nil || g.Topics == nil {
		t.Fatal("NewGraph returned nil maps")
	}

	g.People["a@x.com"] = &Person{ID: "a@x.com", Name: "A", Topics: map[ID]float64{"go": 1}}
	g.Teams["core"] = &Team{ID: "core", Name: "Core"}

	want := &Person{ID: "a@x.com", Name: "A", Topics: map[ID]float64{"go": 1}}
	if diff := cmp.Diff(want, g.People["a@x.com"], cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("person mismatch (-want +got):\n%s", diff)
	}
	if len(g.Teams) != 1 {
		t.Errorf("team count = %d, want 1", len(g.Teams))
	}
}
