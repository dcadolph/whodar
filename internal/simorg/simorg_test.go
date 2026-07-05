package simorg

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dcadolph/whodar/internal/confluence"
	"github.com/dcadolph/whodar/internal/connector"
	"github.com/dcadolph/whodar/internal/feedback"
	"github.com/dcadolph/whodar/internal/github"
	"github.com/dcadolph/whodar/internal/index"
	"github.com/dcadolph/whodar/internal/jira"
	"github.com/dcadolph/whodar/internal/model"
	"github.com/dcadolph/whodar/internal/pagerduty"
	"github.com/dcadolph/whodar/internal/slack"
)

// buildFullIndex ingests every source against the simulated org and returns
// the merged, canonicalized index.
func buildFullIndex(t *testing.T) *index.Index {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()

	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}
	csvPath := write("org.csv", OrgCSV())
	ownersPath := write("CODEOWNERS", CodeOwners())
	aliasPath := write("aliases.json", Aliases())

	repoDir := filepath.Join(dir, "repo")
	if err := BuildGitRepo(repoDir); err != nil {
		t.Fatalf("git repo: %v", err)
	}

	slackSrv := SlackServer()
	t.Cleanup(slackSrv.Close)
	githubSrv := GitHubServer()
	t.Cleanup(githubSrv.Close)
	jiraSrv := JiraServer()
	t.Cleanup(jiraSrv.Close)
	confluenceSrv := ConfluenceServer()
	t.Cleanup(confluenceSrv.Close)
	pagerdutySrv := PagerDutyServer()
	t.Cleanup(pagerdutySrv.Close)

	fetches := []struct {
		Name   string
		Source connector.Source
	}{
		{"org-csv", connector.NewOrgCSV(csvPath)},
		{"codeowners", connector.NewCodeOwners(ownersPath)},
		{"slack", connector.NewSlackWithClient(
			slack.New("xoxb-sim", slack.WithBaseURL(slackSrv.URL)), connector.SlackOptions{})},
		{"github", connector.NewGitHubWithClient(
			github.New("ghp-sim", github.WithBaseURL(githubSrv.URL)),
			connector.GitHubOptions{
				Repos: []string{"corp/billing-service", "corp/webapp"}, ResolveEmails: true,
			})},
		{"jira", connector.NewJiraWithClient(
			jira.New(jiraSrv.URL, "sim@corp.com", "token"), connector.JiraOptions{})},
		{"confluence", connector.NewConfluenceWithClient(
			confluence.New(confluenceSrv.URL, "sim@corp.com", "token"),
			connector.ConfluenceOptions{})},
		{"pagerduty", connector.NewPagerDutyWithClient(
			pagerduty.New("token", pagerduty.WithBaseURL(pagerdutySrv.URL)),
			connector.PagerDutyOptions{})},
		{"git", connector.NewGitHistory(connector.GitOptions{
			Paths: []string{repoDir}, SinceDays: 900,
		})},
	}

	ix := index.New()
	if err := ix.LoadAliases(aliasPath); err != nil {
		t.Fatalf("aliases: %v", err)
	}
	for _, f := range fetches {
		recs, err := f.Source.Fetch(ctx)
		if err != nil {
			t.Fatalf("%s fetch: %v", f.Name, err)
		}
		if len(recs) == 0 {
			t.Fatalf("%s fetch returned no records", f.Name)
		}
		ix.Add(recs)
	}
	ix.Canonicalize()
	return ix
}

// TestFullPipeline runs all eight sources through the real clients against
// wire-format servers and checks the truths that only show up end to end.
func TestFullPipeline(t *testing.T) {
	t.Parallel()
	ix := buildFullIndex(t)

	// One node per human: nine people, no bots, no source-id duplicates.
	if got := len(ix.Graph.People); got != 9 {
		ids := make([]model.ID, 0, got)
		for id := range ix.Graph.People {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		t.Fatalf("people = %d, want 9: %v", got, ids)
	}
	for id := range ix.Graph.People {
		if p := string(id); p == "github:eve-dev" || p == "github:buildbot[bot]" {
			t.Errorf("unmerged or bot identity survived: %s", p)
		}
	}

	// Eve joined through the alias file and remembers her GitHub identity.
	eve := ix.Graph.People["eve@corp.com"]
	if eve == nil {
		t.Fatal("missing eve@corp.com")
	}
	if !slices.Contains(eve.Identities, model.ID("github:eve-dev")) {
		t.Errorf("eve identities = %v, want github:eve-dev", eve.Identities)
	}

	// Cross-source ranking: the right owner tops each question.
	asks := []struct {
		Query      string
		WantPerson model.ID
	}{
		{"billing retries", "jane@corp.com"},
		{"kafka streaming", "bob@corp.com"},
		{"sso login", "dan@corp.com"},
		{"react frontend", "eve@corp.com"},
		{"embeddings model serving", "frank@corp.com"},
		{"terraform", "carol@corp.com"},
	}
	for _, ask := range asks {
		got := ix.Search(ask.Query, 3)
		if len(got) == 0 || got[0].Person.ID != ask.WantPerson {
			t.Errorf("Search(%q) top = %v, want %s", ask.Query, first(got), ask.WantPerson)
			continue
		}
		if got[0].Confidence < 0.45 {
			t.Errorf("Search(%q) confidence = %.2f, want at least moderate",
				ask.Query, got[0].Confidence)
		}
	}

	// Recency: Carol's fresh terraform work outranks Victor's old volume.
	terraform := ix.Search("terraform", 5)
	carolRank, victorRank := rank(terraform, "carol@corp.com"), rank(terraform, "victor@corp.com")
	if carolRank != 1 || victorRank == 0 || victorRank < carolRank {
		t.Errorf("terraform ranks: carol %d victor %d, want carol first and victor present",
			carolRank, victorRank)
	}

	// The right channel surfaces, with the poster as a member.
	channels := ix.SearchChannels("billing retries", 3)
	if len(channels) == 0 || channels[0].Channel.Name != "payments" {
		t.Fatalf("channels top = %+v, want payments", channels)
	}
	if !slices.Contains(channels[0].Channel.Members, model.ID("jane@corp.com")) {
		t.Errorf("payments members = %v, want jane", channels[0].Channel.Members)
	}

	// Feedback tunes but does not bury: votes for Victor keep Carol first.
	ix.SetFeedback([]feedback.Entry{
		{Query: "terraform", Person: "victor@corp.com", Vote: feedback.Helpful},
		{Query: "terraform", Person: "victor@corp.com", Vote: feedback.Helpful},
	})
	after := ix.Search("terraform", 5)
	if after[0].Person.ID != "carol@corp.com" {
		t.Errorf("terraform top after votes = %s; capped feedback must not bury recency",
			after[0].Person.ID)
	}
	if rank(after, "victor@corp.com") > victorRank {
		t.Errorf("victor fell after helpful votes: %d -> %d",
			victorRank, rank(after, "victor@corp.com"))
	}
}

// rank returns the one-based rank of id in matches, or zero when absent.
func rank(matches []model.Match, id model.ID) int {
	for i, m := range matches {
		if m.Person.ID == id {
			return i + 1
		}
	}
	return 0
}

// first names the top match for an error message, or "none".
func first(matches []model.Match) string {
	if len(matches) == 0 {
		return "none"
	}
	return string(matches[0].Person.ID)
}
