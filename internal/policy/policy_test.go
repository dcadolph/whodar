package policy

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestParseMode covers valid names, the empty default, and unknown input.
func TestParseMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		In       string
		WantMode Mode
		Want     error
	}{
		{In: "", WantMode: Strict},                           // Test 0: Empty defaults to strict.
		{In: "strict", WantMode: Strict},                     // Test 1: Strict.
		{In: "Redacted", WantMode: Redacted},                 // Test 2: Case-insensitive.
		{In: " open ", WantMode: Open},                       // Test 3: Trimmed.
		{In: "nope", WantMode: Strict, Want: ErrUnknownMode}, // Test 4: Unknown.
	}
	for testNum, test := range tests {
		t.Run(test.In, func(t *testing.T) {
			t.Parallel()
			got, err := ParseMode(test.In)
			if !errors.Is(err, test.Want) {
				t.Fatalf("test %d: err = %v, want %v", testNum, err, test.Want)
			}
			if diff := cmp.Diff(test.WantMode, got); diff != "" {
				t.Errorf("test %d: mode mismatch (-want +got):\n%s", testNum, diff)
			}
		})
	}
}

// TestAllowEgress verifies Strict denies and looser modes permit.
func TestAllowEgress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Mode Mode
		Want error
	}{
		{Mode: Strict, Want: ErrEgressDenied}, // Test 0: Strict denies.
		{Mode: Redacted, Want: nil},           // Test 1: Redacted permits.
		{Mode: Open, Want: nil},               // Test 2: Open permits.
	}
	for testNum, test := range tests {
		t.Run(test.Mode.String(), func(t *testing.T) {
			t.Parallel()
			err := New(test.Mode, false).AllowEgress("api.example.com", 128)
			if !errors.Is(err, test.Want) {
				t.Errorf("test %d: err = %v, want %v", testNum, err, test.Want)
			}
		})
	}
}

// TestWithModeLocked verifies a locked policy refuses to change mode.
func TestWithModeLocked(t *testing.T) {
	t.Parallel()
	locked := New(Strict, true)
	if _, err := locked.WithMode(Open); !errors.Is(err, ErrLocked) {
		t.Errorf("locked change: err = %v, want ErrLocked", err)
	}
	if got, err := locked.WithMode(Strict); err != nil || got.Mode() != Strict {
		t.Errorf("same-mode change: got %v err %v, want strict nil", got.Mode(), err)
	}

	unlocked := New(Strict, false)
	got, err := unlocked.WithMode(Open)
	if err != nil || got.Mode() != Open {
		t.Errorf("unlocked change: got %v err %v, want open nil", got.Mode(), err)
	}
}
