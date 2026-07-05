package connector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// newFixtureRepo creates a git repository with a small history: Alice touches
// terraform twice, Bob touches python once, and a bot commit that must be
// skipped.
func newFixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	commit := func(rel, content, name, email string, when time.Time) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, err := wt.Add(rel); err != nil {
			t.Fatalf("add: %v", err)
		}
		sig := &object.Signature{Name: name, Email: email, When: when}
		if _, err := wt.Commit("touch "+rel, &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	now := time.Now()
	commit("infra/main.tf", "a", "Alice Smith", "alice@corp.com", now.AddDate(0, 0, -30))
	commit("infra/vpc.tf", "b", "Alice Smith", "alice@corp.com", now.AddDate(0, 0, -10))
	commit("app/serve.py", "c", "Bob Jones", "bob@corp.com", now.AddDate(0, 0, -5))
	commit("go.sum", "d", "dependabot[bot]", "12345+dependabot[bot]@users.noreply.github.com",
		now.AddDate(0, 0, -1))
	return dir
}

func TestGitHistoryFetch(t *testing.T) {
	t.Parallel()
	dir := newFixtureRepo(t)
	recs, err := NewGitHistory(GitOptions{Paths: []string{dir}}).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	byEmail := make(map[string]Record)
	for _, r := range recs {
		byEmail[r.Email] = r
	}
	if len(byEmail) != 2 {
		t.Fatalf("authors = %d (%v), want 2 with the bot skipped", len(byEmail), recs)
	}

	alice := byEmail["alice@corp.com"]
	if !slices.Contains(alice.Topics, "terraform") || !slices.Contains(alice.Topics, "infra") {
		t.Errorf("alice topics = %v, want terraform and infra", alice.Topics)
	}
	if alice.Name != "Alice Smith" {
		t.Errorf("alice name = %q", alice.Name)
	}
	wantLatest := time.Now().AddDate(0, 0, -10)
	if alice.Time.Before(wantLatest.Add(-time.Hour)) || alice.Time.After(wantLatest.Add(time.Hour)) {
		t.Errorf("alice time = %v, want near her latest commit %v", alice.Time, wantLatest)
	}

	bob := byEmail["bob@corp.com"]
	if !slices.Contains(bob.Topics, "python") || !slices.Contains(bob.Topics, "serve") {
		t.Errorf("bob topics = %v, want python and serve", bob.Topics)
	}
}

func TestGitHistoryMaxCommits(t *testing.T) {
	t.Parallel()
	dir := newFixtureRepo(t)
	recs, err := NewGitHistory(GitOptions{Paths: []string{dir}, MaxCommits: 1}).
		Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(recs) != 1 || recs[0].Email != "bob@corp.com" {
		t.Errorf("records = %+v, want only the newest human commit (bob)", recs)
	}
}

func TestGitHistoryErrors(t *testing.T) {
	t.Parallel()
	if _, err := NewGitHistory(GitOptions{}).Fetch(context.Background()); !errors.Is(err, ErrNoRepoPaths) {
		t.Errorf("no paths error = %v, want ErrNoRepoPaths", err)
	}
	dir := t.TempDir()
	if _, err := NewGitHistory(GitOptions{Paths: []string{dir}}).Fetch(context.Background()); err == nil {
		t.Error("want an error for a directory that is not a repository")
	}
}
