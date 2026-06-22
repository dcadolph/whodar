package policy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadMissing verifies an absent file reports found=false without error.
func TestLoadMissing(t *testing.T) {
	t.Parallel()
	_, found, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || found {
		t.Fatalf("found=%v err=%v, want false nil", found, err)
	}
}

// TestLoadAndBuild verifies a locked deny config builds a pinned policy.
func TestLoadAndBuild(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "policy.json")
	body := `{"mode":"strict","locked":true,"private_channels":"deny"}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, found, err := Load(path)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v, want true nil", found, err)
	}
	pol, err := cfg.Policy()
	if err != nil {
		t.Fatalf("Policy: %v", err)
	}
	if pol.Mode() != Strict || !pol.Locked() || pol.AllowPrivateChannels() {
		t.Errorf("policy = mode %s locked %v private %v, want strict locked deny",
			pol.Mode(), pol.Locked(), pol.AllowPrivateChannels())
	}
}

// TestLoadBadJSON verifies malformed content is an error.
func TestLoadBadJSON(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("want error for malformed json, got nil")
	}
}

// TestConfigBadMode verifies an unknown mode name is an error.
func TestConfigBadMode(t *testing.T) {
	t.Parallel()
	if _, err := (Config{Mode: "nonsense"}).Policy(); !errors.Is(err, ErrUnknownMode) {
		t.Fatalf("err = %v, want ErrUnknownMode", err)
	}
}
