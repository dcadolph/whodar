package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/dcadolph/whodar/internal/policy"
)

// writePolicyFile writes a policy config and points the env var at it.
func writePolicyFile(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(policyEnvVar, path)
}

// TestResolvePolicyLockedPins verifies a locked file overrides the user flag.
func TestResolvePolicyLockedPins(t *testing.T) {
	writePolicyFile(t, `{"mode":"strict","locked":true,"private_channels":"deny"}`)
	o := &options{policyName: "open"}
	if err := o.resolvePolicy(true, io.Discard); err != nil {
		t.Fatalf("resolvePolicy: %v", err)
	}
	if o.pol.Mode() != policy.Strict {
		t.Errorf("mode = %s, want strict (pinned over --policy open)", o.pol.Mode())
	}
	if o.pol.AllowPrivateChannels() {
		t.Error("locked policy should deny private-channel ingest")
	}
}

// TestResolvePolicyUnlockedDefault verifies an unlocked file sets the default
// mode when the user did not pass the flag.
func TestResolvePolicyUnlockedDefault(t *testing.T) {
	writePolicyFile(t, `{"mode":"redacted","locked":false}`)
	o := &options{policyName: "strict"}
	if err := o.resolvePolicy(false, io.Discard); err != nil {
		t.Fatalf("resolvePolicy: %v", err)
	}
	if o.pol.Mode() != policy.Redacted {
		t.Errorf("mode = %s, want redacted (file default)", o.pol.Mode())
	}
}

// TestResolvePolicyNoFile verifies the flag governs when no file is present.
func TestResolvePolicyNoFile(t *testing.T) {
	t.Setenv(policyEnvVar, filepath.Join(t.TempDir(), "absent.json"))
	o := &options{policyName: "open"}
	if err := o.resolvePolicy(true, io.Discard); err != nil {
		t.Fatalf("resolvePolicy: %v", err)
	}
	if o.pol.Mode() != policy.Open {
		t.Errorf("mode = %s, want open (flag)", o.pol.Mode())
	}
}
