package identity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dcadolph/whodar/internal/model"
)

func TestCanonical(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Unions [][2]model.ID
		Ask    model.ID
		WantID model.ID
	}{{ // Test 0: Unseen identifier resolves to itself.
		Ask: "alice@corp.com", WantID: "alice@corp.com",
	}, { // Test 1: Email beats a source-prefixed identifier.
		Unions: [][2]model.ID{{"alice@corp.com", "github:alice"}},
		Ask:    "github:alice", WantID: "alice@corp.com",
	}, { // Test 2: Union order does not change the winner.
		Unions: [][2]model.ID{{"github:alice", "alice@corp.com"}},
		Ask:    "github:alice", WantID: "alice@corp.com",
	}, { // Test 3: Email beats a bare name slug.
		Unions: [][2]model.ID{{"alice-smith", "alice@corp.com"}},
		Ask:    "alice-smith", WantID: "alice@corp.com",
	}, { // Test 4: Name slug beats a source-prefixed identifier.
		Unions: [][2]model.ID{{"github:alice", "alice-smith"}},
		Ask:    "github:alice", WantID: "alice-smith",
	}, { // Test 5: Rank ties break lexically.
		Unions: [][2]model.ID{{"bob@corp.com", "alice@corp.com"}},
		Ask:    "bob@corp.com", WantID: "alice@corp.com",
	}, { // Test 6: Transitive chains resolve through the whole set.
		Unions: [][2]model.ID{
			{"github:alice", "codeowners:alice"},
			{"codeowners:alice", "alice@corp.com"},
		},
		Ask: "github:alice", WantID: "alice@corp.com",
	}, { // Test 7: Identifiers normalize before matching.
		Unions: [][2]model.ID{{"Alice@Corp.com ", "GitHub:Alice"}},
		Ask:    "github:alice", WantID: "alice@corp.com",
	}, { // Test 8: Self and empty unions are ignored.
		Unions: [][2]model.ID{{"alice@corp.com", "alice@corp.com"}, {"", "alice@corp.com"}},
		Ask:    "alice@corp.com", WantID: "alice@corp.com",
	}}
	for testNum, test := range tests {
		t.Run(fmt.Sprintf("test %d", testNum), func(t *testing.T) {
			t.Parallel()
			r := NewResolver()
			for _, u := range test.Unions {
				r.Union(u[0], u[1])
			}
			if got := r.Canonical(test.Ask); got != test.WantID {
				t.Errorf("Canonical(%q) = %q, want %q", test.Ask, got, test.WantID)
			}
		})
	}
}

func TestPairsRestore(t *testing.T) {
	t.Parallel()
	r := NewResolver()
	r.Union("alice@corp.com", "github:alice")
	r.Union("alice@corp.com", "jira:abc123")
	r.Union("bob@corp.com", "pagerduty:pxyz")

	want := map[model.ID]model.ID{
		"github:alice":   "alice@corp.com",
		"jira:abc123":    "alice@corp.com",
		"pagerduty:pxyz": "bob@corp.com",
	}
	if diff := cmp.Diff(want, r.Pairs()); diff != "" {
		t.Fatalf("pairs mismatch (-want +got):\n%s", diff)
	}

	restored := NewResolver()
	restored.Restore(r.Pairs())
	if diff := cmp.Diff(want, restored.Pairs()); diff != "" {
		t.Errorf("restored pairs mismatch (-want +got):\n%s", diff)
	}
	if got := restored.Canonical("github:alice"); got != "alice@corp.com" {
		t.Errorf("restored Canonical = %q, want alice@corp.com", got)
	}
}

func TestPairsEmpty(t *testing.T) {
	t.Parallel()
	r := NewResolver()
	if got := r.Pairs(); got != nil {
		t.Errorf("empty resolver Pairs = %v, want nil", got)
	}
	r.Canonical("alice@corp.com")
	if got := r.Pairs(); got != nil {
		t.Errorf("lookup-only resolver Pairs = %v, want nil", got)
	}
}

func TestLoadFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Content string
		Missing bool
		Ask     model.ID
		WantID  model.ID
		Want    error
	}{{ // Test 0: Groups union each alias to its canonical.
		Content: `{"alice@corp.com": ["github:alice", "codeowners:alice"]}`,
		Ask:     "codeowners:alice", WantID: "alice@corp.com",
	}, { // Test 1: Missing file returns ErrAliases.
		Missing: true, Want: ErrAliases,
	}, { // Test 2: Invalid JSON returns ErrAliases.
		Content: "{not json", Want: ErrAliases,
	}}
	for testNum, test := range tests {
		t.Run(fmt.Sprintf("test %d", testNum), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "aliases.json")
			if !test.Missing {
				if err := os.WriteFile(path, []byte(test.Content), 0o600); err != nil {
					t.Fatalf("write fixture: %v", err)
				}
			}
			r := NewResolver()
			err := r.LoadFile(path)
			if !errors.Is(err, test.Want) {
				t.Fatalf("LoadFile error = %v, want %v", err, test.Want)
			}
			if test.Want == nil {
				if got := r.Canonical(test.Ask); got != test.WantID {
					t.Errorf("Canonical(%q) = %q, want %q", test.Ask, got, test.WantID)
				}
			}
		})
	}
}
