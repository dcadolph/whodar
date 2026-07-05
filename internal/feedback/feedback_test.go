package feedback

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestStoreRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "feedback.json")

	s, err := Load(path)
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	when := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	entries := []Entry{
		{Query: "billing retries", Person: "alice@corp.com", Vote: Helpful, Time: when},
		{Query: "billing retries", Channel: "payments", Vote: NotHelpful, Time: when},
	}
	for _, e := range entries {
		if err := s.Add(e); err != nil {
			t.Fatalf("add: %v", err)
		}
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if diff := cmp.Diff(entries, reloaded.All()); diff != "" {
		t.Errorf("entries mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "feedback.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Error("want a parse error for invalid JSON")
	}
}

func TestEntryValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		In         Entry
		WantResult bool
	}{{ // Test 0: A person vote is valid.
		In: Entry{Query: "q", Person: "p", Vote: Helpful}, WantResult: true,
	}, { // Test 1: A channel vote is valid.
		In: Entry{Query: "q", Channel: "c", Vote: NotHelpful}, WantResult: true,
	}, { // Test 2: A missing query is invalid.
		In: Entry{Person: "p", Vote: Helpful},
	}, { // Test 3: Both targets set is invalid.
		In: Entry{Query: "q", Person: "p", Channel: "c", Vote: Helpful},
	}, { // Test 4: No target is invalid.
		In: Entry{Query: "q", Vote: Helpful},
	}, { // Test 5: An unknown vote value is invalid.
		In: Entry{Query: "q", Person: "p", Vote: 2},
	}}
	for testNum, test := range tests {
		t.Run(fmt.Sprintf("test %d", testNum), func(t *testing.T) {
			t.Parallel()
			if got := test.In.Valid(); got != test.WantResult {
				t.Errorf("Valid() = %v, want %v", got, test.WantResult)
			}
		})
	}
}

func TestAddRejectsBadEntry(t *testing.T) {
	t.Parallel()
	s, err := Load(filepath.Join(t.TempDir(), "feedback.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := s.Add(Entry{Query: "q", Vote: Helpful}); !errors.Is(err, ErrBadEntry) {
		t.Errorf("add error = %v, want ErrBadEntry", err)
	}
}
