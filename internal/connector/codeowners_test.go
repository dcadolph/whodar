package connector

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// TestParseCodeOwners covers owner parsing, topic derivation, and identity.
func TestParseCodeOwners(t *testing.T) {
	t.Parallel()
	in := "# comment\n" +
		"/internal/billing/   @jane  @org/payments\n" +
		"*.go                 jane@x.com\n" +
		"/internal/infra/     @kim\n"

	recs, err := parseCodeOwners(context.Background(), strings.NewReader(in))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	byName := make(map[string]Record)
	for _, r := range recs {
		byName[r.Name] = r
	}

	if jane := byName["@jane"]; jane.PersonID != "codeowners:jane" ||
		!slices.Contains(jane.Topics, "billing") {
		t.Errorf("@jane = %+v, want id codeowners:jane and topic billing", jane)
	}
	if team := byName["@org/payments"]; team.PersonID != "codeowners:org/payments" {
		t.Errorf("@org/payments id = %q", team.PersonID)
	}
	if email := byName["jane@x.com"]; email.Email != "jane@x.com" {
		t.Errorf("email owner = %q, want jane@x.com", email.Email)
	}
	if kim := byName["@kim"]; !slices.Contains(kim.Topics, "infra") {
		t.Errorf("@kim topics = %v, want infra", kim.Topics)
	}
}

// TestCodeOwnersMissing verifies a directory with no CODEOWNERS is an error.
func TestCodeOwnersMissing(t *testing.T) {
	t.Parallel()
	if _, err := NewCodeOwners(t.TempDir()).Fetch(context.Background()); !errors.Is(err, ErrNoCodeOwners) {
		t.Fatalf("err = %v, want ErrNoCodeOwners", err)
	}
}
