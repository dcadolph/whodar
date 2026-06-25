package connector

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/dcadolph/whodar/internal/jira"
)

// JiraOptions configures the Jira connector.
type JiraOptions struct {
	// Projects scopes the search to these project keys.
	Projects []string
	// JQL overrides the query entirely when set.
	JQL string
	// MaxIssues caps issues read; zero uses a default.
	MaxIssues int
	// Log receives progress lines; nil discards them.
	Log io.Writer
}

// withDefaults fills the log writer and issue cap when unset.
func (o JiraOptions) withDefaults() JiraOptions {
	if o.Log == nil {
		o.Log = io.Discard
	}
	if o.MaxIssues <= 0 {
		o.MaxIssues = 1000
	}
	return o
}

// Jira is a Source that ingests issues and weights the assignee and reporter by
// the components, labels, summary words, and project of the issues they handle.
type Jira struct {
	// client calls the Jira API.
	client *jira.Client
	// opts holds the resolved options.
	opts JiraOptions
}

// NewJira returns a Jira connector for the site, authenticating with an email
// and API token.
func NewJira(baseURL, email, token string, opts JiraOptions) *Jira {
	return &Jira{client: jira.New(baseURL, email, token), opts: opts.withDefaults()}
}

// NewJiraWithClient returns a Jira connector using a preconfigured client.
// Tests use it to inject a client pointed at a mock server.
func NewJiraWithClient(client *jira.Client, opts JiraOptions) *Jira {
	if client == nil {
		panic("connector: NewJiraWithClient requires a non-nil client")
	}
	return &Jira{client: client, opts: opts.withDefaults()}
}

// Fetch searches issues and returns one record per person, weighted by topic.
func (j *Jira) Fetch(ctx context.Context) ([]Record, error) {
	query := j.jql()
	issues, err := j.client.Search(ctx, query, j.opts.MaxIssues)
	if err != nil {
		return nil, fmt.Errorf("jira search: %w", err)
	}
	fmt.Fprintf(j.opts.Log, "jira: %d issues for %q\n", len(issues), query)

	counts := make(map[string]map[string]int)
	users := make(map[string]jira.User)
	bump := func(u *jira.User, tokens []string) {
		if u == nil {
			return
		}
		key := jiraUserKey(*u)
		if key == "" {
			return
		}
		c := counts[key]
		if c == nil {
			c = make(map[string]int)
			counts[key] = c
		}
		for _, t := range tokens {
			if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
				c[t]++
			}
		}
		users[key] = *u
	}

	for _, is := range issues {
		tokens := issueTopics(is)
		bump(is.Fields.Assignee, tokens)
		bump(is.Fields.Reporter, tokens)
	}

	records := make([]Record, 0, len(counts))
	for key, c := range counts {
		records = append(records, jiraPersonRecord(users[key], expandTopics(c)))
	}
	return records, nil
}

// jql returns the query: an explicit JQL, or a project scope, or all issues.
func (j *Jira) jql() string {
	if strings.TrimSpace(j.opts.JQL) != "" {
		return j.opts.JQL
	}
	if len(j.opts.Projects) > 0 {
		quoted := make([]string, len(j.opts.Projects))
		for i, p := range j.opts.Projects {
			quoted[i] = `"` + p + `"`
		}
		return "project in (" + strings.Join(quoted, ",") + ") ORDER BY updated DESC"
	}
	return "ORDER BY updated DESC"
}

// issueTopics derives topic tokens from an issue's components, labels, summary,
// and project name.
func issueTopics(is jira.Issue) []string {
	f := is.Fields
	var out []string
	for _, c := range f.Components {
		out = append(out, titleTokens(c.Name)...)
	}
	out = append(out, f.Labels...)
	out = append(out, titleTokens(f.Summary)...)
	out = append(out, titleTokens(f.Project.Name)...)
	return out
}

// jiraUserKey returns a stable key for a user, preferring email.
func jiraUserKey(u jira.User) string {
	if u.EmailAddress != "" {
		return strings.ToLower(u.EmailAddress)
	}
	if u.AccountID != "" {
		return "jira:" + u.AccountID
	}
	return ""
}

// jiraPersonRecord builds a person record. An email lets the person join other
// sources; otherwise the account id keys the record.
func jiraPersonRecord(u jira.User, topics []string) Record {
	rec := Record{Kind: KindPerson, Source: "jira", Weight: 1, Topics: topics, Name: u.DisplayName}
	if u.EmailAddress != "" {
		rec.Email = strings.ToLower(u.EmailAddress)
	} else {
		rec.PersonID = "jira:" + u.AccountID
	}
	if rec.Name == "" {
		if rec.Email != "" {
			rec.Name = rec.Email
		} else {
			rec.Name = rec.PersonID
		}
	}
	return rec
}
