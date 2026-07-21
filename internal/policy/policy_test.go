package policy

import (
	"errors"
	"fmt"
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

// TestAllowEgress verifies Strict denies everything, Redacted permits only
// known model providers, and Open permits any destination.
func TestAllowEgress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Mode Mode
		Dest string
		Want error
	}{
		{Mode: Strict, Dest: "api.anthropic.com", Want: ErrEgressDenied},          // Test 0: Strict denies providers too.
		{Mode: Redacted, Dest: "api.anthropic.com", Want: nil},                    // Test 1: Redacted permits Anthropic.
		{Mode: Redacted, Dest: "api.openai.com", Want: nil},                       // Test 2: Redacted permits OpenAI.
		{Mode: Redacted, Dest: "generativelanguage.googleapis.com", Want: nil},    // Test 3: Redacted permits Gemini.
		{Mode: Redacted, Dest: "llm.corp.example", Want: ErrEgressDenied},         // Test 4: Redacted denies unknown hosts.
		{Mode: Open, Dest: "llm.corp.example", Want: nil},                         // Test 5: Open permits anything.
	}
	for testNum, test := range tests {
		t.Run(fmt.Sprintf("test %d", testNum), func(t *testing.T) {
			t.Parallel()
			err := New(test.Mode, false).AllowEgress(test.Dest)
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

// TestPrivateChannels verifies private ingest defaults on and can be pinned off.
func TestPrivateChannels(t *testing.T) {
	t.Parallel()
	if !New(Strict, false).AllowPrivateChannels() {
		t.Error("default policy should allow private-channel ingest")
	}
	if New(Strict, false).WithoutPrivateChannels().AllowPrivateChannels() {
		t.Error("pinned-off policy should forbid private-channel ingest")
	}
}
