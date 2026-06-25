package connector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/dcadolph/whodar/internal/github"
)

// ErrNoRepos indicates no repositories were given to the GitHub connector.
var ErrNoRepos = errors.New("github: no repositories (use repos or an org)")

// GitHubOptions configures the GitHub connector.
type GitHubOptions struct {
	// Repos is a list of "owner/name" repositories to index.
	Repos []string
	// Org, when set, adds the org's repositories.
	Org string
	// MaxRepos caps repositories taken from the org; zero means all returned.
	MaxRepos int
	// ResolveEmails fetches each user's profile to join by email.
	ResolveEmails bool
	// Log receives progress lines; nil discards them.
	Log io.Writer
}

// withDefaults fills the log writer when unset.
func (o GitHubOptions) withDefaults() GitHubOptions {
	if o.Log == nil {
		o.Log = io.Discard
	}
	return o
}

// GitHub is a Source that ingests repositories: contributors, pull request
// authors and reviewers, repository topics, and CODEOWNERS.
type GitHub struct {
	// client calls the GitHub API.
	client *github.Client
	// opts holds the resolved options.
	opts GitHubOptions
}

// NewGitHub returns a GitHub connector authenticating with token.
func NewGitHub(token string, opts GitHubOptions) *GitHub {
	return &GitHub{client: github.New(token), opts: opts.withDefaults()}
}

// NewGitHubWithClient returns a GitHub connector using a preconfigured client.
// Tests use it to inject a client pointed at a mock server.
func NewGitHubWithClient(client *github.Client, opts GitHubOptions) *GitHub {
	if client == nil {
		panic("connector: NewGitHubWithClient requires a non-nil client")
	}
	return &GitHub{client: client, opts: opts.withDefaults()}
}

// Fetch reads each repository and returns person records for contributors,
// pull request authors and reviewers, and CODEOWNERS owners.
func (g *GitHub) Fetch(ctx context.Context) ([]Record, error) {
	repos, err := g.repoList(ctx)
	if err != nil {
		return nil, err
	}

	topics := make(map[string]map[string]bool) // login -> topic set
	add := func(login string, ts ...string) {
		if login == "" {
			return
		}
		if topics[login] == nil {
			topics[login] = make(map[string]bool)
		}
		for _, t := range ts {
			if t = strings.TrimSpace(strings.ToLower(t)); t != "" {
				topics[login][t] = true
			}
		}
	}

	var codeOwnerRecords []Record
	for _, full := range repos {
		owner, name, ok := splitRepo(full)
		if !ok {
			continue
		}
		repo, err := g.client.Repo(ctx, owner, name)
		if err != nil {
			return nil, fmt.Errorf("github repo %s: %w", full, err)
		}
		repoTopics := repoTopicSet(repo)

		cons, err := g.client.Contributors(ctx, owner, name)
		if err != nil {
			return nil, fmt.Errorf("github contributors %s: %w", full, err)
		}
		for _, c := range cons {
			add(c.Login, repoTopics...)
		}

		pulls, err := g.client.PullRequests(ctx, owner, name)
		if err != nil {
			return nil, fmt.Errorf("github pulls %s: %w", full, err)
		}
		for _, pr := range pulls {
			labels := pr.LabelNames()
			add(pr.Author(), repoTopics...)
			add(pr.Author(), labels...)
			for _, rev := range pr.Reviewers() {
				add(rev, labels...)
			}
		}

		if content := g.codeOwners(ctx, owner, name); content != nil {
			if recs, err := parseCodeOwners(ctx, bytes.NewReader(content)); err == nil {
				codeOwnerRecords = append(codeOwnerRecords, recs...)
			}
		}
		fmt.Fprintf(g.opts.Log, "github: indexed %s (%d contributors, %d pulls)\n", full, len(cons), len(pulls))
	}

	accounts := g.resolveAccounts(ctx, topics)

	records := make([]Record, 0, len(topics)+len(codeOwnerRecords))
	for login, set := range topics {
		records = append(records, githubPersonRecord(login, set, accounts[login]))
	}
	records = append(records, codeOwnerRecords...)
	return records, nil
}

// repoList resolves the explicit repos plus any from the org, capped.
func (g *GitHub) repoList(ctx context.Context) ([]string, error) {
	repos := append([]string(nil), g.opts.Repos...)
	if g.opts.Org != "" {
		orgRepos, err := g.client.OrgRepos(ctx, g.opts.Org)
		if err != nil {
			return nil, fmt.Errorf("github org %s: %w", g.opts.Org, err)
		}
		for i, r := range orgRepos {
			if g.opts.MaxRepos > 0 && i >= g.opts.MaxRepos {
				fmt.Fprintf(g.opts.Log, "github: stopping at %d org repos (cap)\n", g.opts.MaxRepos)
				break
			}
			repos = append(repos, r.FullName)
		}
	}
	if len(repos) == 0 {
		return nil, ErrNoRepos
	}
	return repos, nil
}

// resolveAccounts looks up each login's profile when email resolution is on.
func (g *GitHub) resolveAccounts(ctx context.Context, logins map[string]map[string]bool) map[string]github.Account {
	accounts := make(map[string]github.Account)
	if !g.opts.ResolveEmails {
		return accounts
	}
	for login := range logins {
		if a, err := g.client.Account(ctx, login); err == nil {
			accounts[login] = a
		}
	}
	return accounts
}

// codeOwners returns the first CODEOWNERS file found in the repo, or nil.
func (g *GitHub) codeOwners(ctx context.Context, owner, name string) []byte {
	for _, p := range []string{"CODEOWNERS", ".github/CODEOWNERS", "docs/CODEOWNERS"} {
		if content, err := g.client.FileContents(ctx, owner, name, p); err == nil {
			return content
		}
	}
	return nil
}

// githubPersonRecord builds a person record. A resolved email lets the person
// join other sources; otherwise the handle keys the record.
func githubPersonRecord(login string, topicSet map[string]bool, a github.Account) Record {
	rec := Record{Kind: KindPerson, Source: "github", Weight: 1, Topics: sortedKeys(topicSet)}
	if a.Email != "" {
		rec.Email = strings.ToLower(a.Email)
		rec.Name = a.Name
		if rec.Name == "" {
			rec.Name = "@" + login
		}
		return rec
	}
	rec.PersonID = "github:" + strings.ToLower(login)
	rec.Name = "@" + login
	if a.Name != "" {
		rec.Name = a.Name
	}
	return rec
}

// repoTopicSet derives a repo's topic tags from its GitHub topics and the words
// of its name.
func repoTopicSet(repo github.Repo) []string {
	out := append([]string(nil), repo.Topics...)
	for _, part := range strings.FieldsFunc(repo.Name, func(r rune) bool {
		return r == '-' || r == '_' || r == '/' || r == ' '
	}) {
		if len(part) >= 3 && !codeStop[strings.ToLower(part)] {
			out = append(out, part)
		}
	}
	return out
}

// splitRepo splits "owner/name" into its parts.
func splitRepo(full string) (owner, name string, ok bool) {
	owner, name, ok = strings.Cut(strings.TrimSpace(full), "/")
	if !ok || owner == "" || name == "" {
		return "", "", false
	}
	return owner, name, true
}

// sortedKeys returns the map keys sorted, for deterministic output.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
