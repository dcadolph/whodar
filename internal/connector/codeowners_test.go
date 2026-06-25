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
		"*.tf                 @kim\n" +
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
	if email := byName["jane@x.com"]; email.Email != "jane@x.com" ||
		!slices.Contains(email.Topics, "golang") {
		t.Errorf("email owner = %+v, want jane@x.com with golang from *.go", email)
	}
	if kim := byName["@kim"]; !slices.Contains(kim.Topics, "infra") ||
		!slices.Contains(kim.Topics, "terraform") {
		t.Errorf("@kim topics = %v, want infra and terraform", kim.Topics)
	}
}

// TestCodeOwnersMissing verifies a directory with no CODEOWNERS is an error.
func TestCodeOwnersMissing(t *testing.T) {
	t.Parallel()
	if _, err := NewCodeOwners(t.TempDir()).Fetch(context.Background()); !errors.Is(err, ErrNoCodeOwners) {
		t.Fatalf("err = %v, want ErrNoCodeOwners", err)
	}
}
