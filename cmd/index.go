package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dcadolph/whodar/internal/connector"
	"github.com/dcadolph/whodar/internal/index"
	"github.com/dcadolph/whodar/internal/model"
)

// slackTokenEnv is the environment variable holding the Slack bot token.
const slackTokenEnv = "WHODAR_SLACK_TOKEN"

// githubTokenEnv is the environment variable holding the GitHub token.
const githubTokenEnv = "WHODAR_GITHUB_TOKEN"

// pagerdutyTokenEnv is the environment variable holding the PagerDuty token.
const pagerdutyTokenEnv = "WHODAR_PAGERDUTY_TOKEN"

// Jira environment variables for the site URL, email, and API token.
const (
	jiraURLEnv   = "WHODAR_JIRA_URL"
	jiraEmailEnv = "WHODAR_JIRA_EMAIL"
	jiraTokenEnv = "WHODAR_JIRA_TOKEN"
)

// Confluence environment variables. They fall back to the Jira ones because
// both use the same Atlassian site and token.
const (
	confluenceURLEnv   = "WHODAR_CONFLUENCE_URL"
	confluenceEmailEnv = "WHODAR_CONFLUENCE_EMAIL"
	confluenceTokenEnv = "WHODAR_CONFLUENCE_TOKEN"
)

// newIndexCmd builds the index command, which ingests a source into the index.
func newIndexCmd(opts *options) *cobra.Command {
	var (
		source           string
		file             string
		includePrivate   bool
		sinceDays        int
		maxMessages      int
		changesFile      string
		embed            bool
		embedModel       string
		ollamaURL        string
		repos            []string
		githubOrg        string
		maxRepos         int
		githubEmails     bool
		jiraURL          string
		jiraProjects     []string
		jiraJQL          string
		maxIssues        int
		merge            bool
		aliasesFile      string
		halfLifeDays     int
		confluenceSpaces []string
		confluenceCQL    string
		maxPages         int
	)
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Build the index from a source",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var (
				recs []connector.Record
				err  error
			)
			switch source {
			case "org-csv":
				if file == "" {
					return fmt.Errorf("%w: --file is required for org-csv", ErrBadArgs)
				}
				recs, err = connector.NewOrgCSV(file).Fetch(cmd.Context())
			case "slack":
				recs, err = fetchSlack(cmd, opts, slackArgs{includePrivate, sinceDays, maxMessages})
			case "codeowners":
				if file == "" {
					return fmt.Errorf("%w: --file (CODEOWNERS path or repo root) required for codeowners", ErrBadArgs)
				}
				recs, err = connector.NewCodeOwners(file).Fetch(cmd.Context())
			case "github":
				recs, err = fetchGitHub(cmd, githubArgs{repos, githubOrg, maxRepos, githubEmails})
			case "jira":
				recs, err = fetchJira(cmd, jiraArgs{jiraURL, jiraProjects, jiraJQL, maxIssues})
			case "confluence":
				recs, err = fetchConfluence(cmd, confluenceArgs{confluenceSpaces, confluenceCQL, maxPages})
			case "pagerduty":
				recs, err = fetchPagerDuty(cmd)
			default:
				return fmt.Errorf("%w: %q (want org-csv, slack, codeowners, github, jira, confluence, or pagerduty)", ErrUnknownSource, source)
			}
			if err != nil {
				return err
			}

			var prev *model.Graph
			if old, lerr := index.Load(opts.indexPath()); lerr == nil {
				prev = old.Graph
			}

			ix := index.New()
			if merge {
				if base, lerr := index.Load(opts.indexPath()); lerr == nil {
					ix = base
				}
			}
			ix.SetHalfLife(time.Duration(halfLifeDays) * 24 * time.Hour)
			if aliasesFile != "" {
				if err := ix.LoadAliases(aliasesFile); err != nil {
					return err
				}
			}
			if merge {
				ix.Add(recs)
			} else {
				ix.Build(recs)
			}
			ix.Canonicalize()

			if embed {
				if err := guardLLMHost(opts.pol, ollamaURL); err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(),
					"embedding %d people and %d channels via Ollama...\n",
					len(ix.Graph.People), len(ix.Graph.Channels))
				if err := ix.Embed(cmd.Context(), newOllama("", embedModel, ollamaURL)); err != nil {
					return fmt.Errorf("embed: %w", err)
				}
			}

			changes := index.Diff(prev, ix.Graph)
			if err := ix.Save(opts.indexPath()); err != nil {
				return err
			}

			out := cmd.ErrOrStderr()
			fmt.Fprintf(out,
				"indexed %d people, %d channels, %d teams, %d topics into %s\n",
				len(ix.Graph.People), len(ix.Graph.Channels), len(ix.Graph.Teams),
				len(ix.Graph.Topics), opts.indexPath())
			reportChanges(out, changes)
			if changesFile != "" {
				if err := writeChangesFile(changesFile, changes); err != nil {
					return err
				}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&source, "source", "org-csv", "Source type: org-csv, slack, codeowners, github, jira, confluence, or pagerduty.")
	f.StringVar(&file, "file", "", "Path to the source file (org-csv).")
	f.BoolVar(&includePrivate, "include-private", false, "Ingest private Slack channels if policy allows.")
	f.IntVar(&sinceDays, "since-days", 180, "Slack history window in days.")
	f.IntVar(&maxMessages, "max-messages", 5000, "Slack message cap per channel.")
	f.StringVar(&changesFile, "changes-file", "", "Write the index diff as JSON to this path.")
	f.BoolVar(&merge, "merge", false, "Merge into the existing index instead of replacing it.")
	f.StringVar(&aliasesFile, "aliases", "",
		"JSON file mapping a canonical id to its aliases, joining one person across sources.")
	f.IntVar(&halfLifeDays, "half-life-days", 180,
		"Days for a dated record's weight to halve; 0 disables recency decay.")
	f.BoolVar(&embed, "embed", false, "Generate embeddings via Ollama for semantic search.")
	f.StringVar(&embedModel, "embed-model", "", "Ollama embed model (default nomic-embed-text).")
	f.StringVar(&ollamaURL, "ollama-url", "http://localhost:11434", "Ollama base URL for --embed.")
	f.StringArrayVar(&repos, "repo", nil, "GitHub repo owner/name (repeatable).")
	f.StringVar(&githubOrg, "github-org", "", "GitHub org to index all repositories of.")
	f.IntVar(&maxRepos, "max-repos", 0, "Cap repositories taken from --github-org (0 = all).")
	f.BoolVar(&githubEmails, "github-emails", false, "Resolve GitHub user emails to join other sources.")
	f.StringVar(&jiraURL, "jira-url", "", "Jira site URL (or WHODAR_JIRA_URL).")
	f.StringArrayVar(&jiraProjects, "jira-project", nil, "Jira project key (repeatable).")
	f.StringVar(&jiraJQL, "jira-jql", "", "Jira JQL query (overrides --jira-project).")
	f.IntVar(&maxIssues, "max-issues", 1000, "Cap Jira issues read.")
	f.StringArrayVar(&confluenceSpaces, "confluence-space", nil, "Confluence space key (repeatable).")
	f.StringVar(&confluenceCQL, "confluence-cql", "", "Confluence CQL query (overrides --confluence-space).")
	f.IntVar(&maxPages, "max-pages", 2000, "Cap Confluence pages read.")
	return cmd
}

// slackArgs holds the Slack-specific index flags.
type slackArgs struct {
	// includePrivate requests private-channel ingest.
	includePrivate bool
	// sinceDays is the history window in days.
	sinceDays int
	// maxMessages caps messages per channel.
	maxMessages int
}

// fetchSlack builds Slack records, enforcing the private-channel policy guard.
func fetchSlack(cmd *cobra.Command, opts *options, a slackArgs) ([]connector.Record, error) {
	token := os.Getenv(slackTokenEnv)
	if token == "" {
		return nil, fmt.Errorf("%w: set %s", ErrBadArgs, slackTokenEnv)
	}
	if a.includePrivate && !opts.pol.AllowPrivateChannels() {
		return nil, fmt.Errorf("%w: private-channel ingest is disabled by policy", ErrBadArgs)
	}
	src := connector.NewSlack(token, connector.SlackOptions{
		IncludePrivate: a.includePrivate,
		SinceDays:      a.sinceDays,
		MaxMessages:    a.maxMessages,
		Log:            cmd.ErrOrStderr(),
	})
	return src.Fetch(cmd.Context())
}

// githubArgs holds the GitHub-specific index flags.
type githubArgs struct {
	// repos is the list of owner/name repositories.
	repos []string
	// org adds every repository in the org.
	org string
	// maxRepos caps repositories taken from the org.
	maxRepos int
	// emails resolves user emails to join other sources.
	emails bool
}

// fetchGitHub builds GitHub records from the configured repositories or org.
func fetchGitHub(cmd *cobra.Command, a githubArgs) ([]connector.Record, error) {
	token := os.Getenv(githubTokenEnv)
	if token == "" {
		return nil, fmt.Errorf("%w: set %s", ErrBadArgs, githubTokenEnv)
	}
	if len(a.repos) == 0 && a.org == "" {
		return nil, fmt.Errorf("%w: --repo or --github-org required for github", ErrBadArgs)
	}
	src := connector.NewGitHub(token, connector.GitHubOptions{
		Repos: a.repos, Org: a.org, MaxRepos: a.maxRepos, ResolveEmails: a.emails, Log: cmd.ErrOrStderr(),
	})
	return src.Fetch(cmd.Context())
}

// jiraArgs holds the Jira-specific index flags.
type jiraArgs struct {
	// url is the Jira site URL.
	url string
	// projects scopes the search to these project keys.
	projects []string
	// jql overrides the query.
	jql string
	// maxIssues caps issues read.
	maxIssues int
}

// fetchJira builds Jira records, reading the URL and credentials from flags and
// the environment.
func fetchJira(cmd *cobra.Command, a jiraArgs) ([]connector.Record, error) {
	site := a.url
	if site == "" {
		site = os.Getenv(jiraURLEnv)
	}
	email := os.Getenv(jiraEmailEnv)
	token := os.Getenv(jiraTokenEnv)
	if site == "" || email == "" || token == "" {
		return nil, fmt.Errorf("%w: set --jira-url (or %s), %s, and %s",
			ErrBadArgs, jiraURLEnv, jiraEmailEnv, jiraTokenEnv)
	}
	src := connector.NewJira(site, email, token, connector.JiraOptions{
		Projects: a.projects, JQL: a.jql, MaxIssues: a.maxIssues, Log: cmd.ErrOrStderr(),
	})
	return src.Fetch(cmd.Context())
}

// confluenceArgs holds the Confluence-specific index flags.
type confluenceArgs struct {
	// spaces scopes the search to these space keys.
	spaces []string
	// cql overrides the query.
	cql string
	// maxPages caps pages read.
	maxPages int
}

// fetchConfluence builds Confluence records. The URL and credentials fall back
// to the Jira environment variables, since both use the same Atlassian site.
func fetchConfluence(cmd *cobra.Command, a confluenceArgs) ([]connector.Record, error) {
	site := firstNonEmpty(os.Getenv(confluenceURLEnv), os.Getenv(jiraURLEnv))
	email := firstNonEmpty(os.Getenv(confluenceEmailEnv), os.Getenv(jiraEmailEnv))
	token := firstNonEmpty(os.Getenv(confluenceTokenEnv), os.Getenv(jiraTokenEnv))
	if site == "" || email == "" || token == "" {
		return nil, fmt.Errorf("%w: set WHODAR_CONFLUENCE_URL, EMAIL, and TOKEN (or the Jira ones)", ErrBadArgs)
	}
	src := connector.NewConfluence(site, email, token, connector.ConfluenceOptions{
		Spaces: a.spaces, CQL: a.cql, MaxPages: a.maxPages, Log: cmd.ErrOrStderr(),
	})
	return src.Fetch(cmd.Context())
}

// fetchPagerDuty builds PagerDuty records from services and on-call data.
func fetchPagerDuty(cmd *cobra.Command) ([]connector.Record, error) {
	token := os.Getenv(pagerdutyTokenEnv)
	if token == "" {
		return nil, fmt.Errorf("%w: set %s", ErrBadArgs, pagerdutyTokenEnv)
	}
	src := connector.NewPagerDuty(token, connector.PagerDutyOptions{Log: cmd.ErrOrStderr()})
	return src.Fetch(cmd.Context())
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// reportChanges prints a one-line summary and capped lists of who and what
// joined or left since the last index.
func reportChanges(w io.Writer, c index.Changes) {
	if c.Empty() {
		return
	}
	fmt.Fprintf(w, "changes since last index: %s\n", c.Summary())
	printChangeList(w, "joined", c.PeopleJoined)
	printChangeList(w, "left", c.PeopleLeft)
	printChangeList(w, "new channels", c.ChannelsAdded)
	printChangeList(w, "gone channels", c.ChannelsRemoved)
}

// printChangeList prints up to a fixed number of items under a label, noting
// any remainder.
func printChangeList(w io.Writer, label string, items []string) {
	const limit = 15
	if len(items) == 0 {
		return
	}
	shown := items
	if len(shown) > limit {
		shown = shown[:limit]
	}
	fmt.Fprintf(w, "  %s: %s", label, strings.Join(shown, ", "))
	if len(items) > limit {
		fmt.Fprintf(w, ", and %d more", len(items)-limit)
	}
	fmt.Fprintln(w)
}

// writeChangesFile writes the changes as JSON to path.
func writeChangesFile(path string, c index.Changes) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("changes file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("changes file: close: %w", cerr)
		}
	}()
	return writeJSON(f, c, true)
}
