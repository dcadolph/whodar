package connector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// Git history ingest bounds. A year of history captures current ownership;
// the commit cap keeps one huge repository from dominating a run.
const (
	// defaultGitSinceDays bounds how far back history is read.
	defaultGitSinceDays = 365
	// defaultMaxCommits caps commits read per repository.
	defaultMaxCommits = 2000
)

// ErrNoRepoPaths indicates no repository paths were given to the git connector.
var ErrNoRepoPaths = errors.New("git: no repository paths")

// GitOptions configures the git history connector.
type GitOptions struct {
	// Paths are local repository roots to read.
	Paths []string
	// SinceDays bounds how far back to read history; zero means one year.
	SinceDays int
	// MaxCommits caps commits read per repository; zero means 2000.
	MaxCommits int
	// Log receives progress lines; nil discards them.
	Log io.Writer
}

// withDefaults fills unset options.
func (o GitOptions) withDefaults() GitOptions {
	if o.SinceDays <= 0 {
		o.SinceDays = defaultGitSinceDays
	}
	if o.MaxCommits <= 0 {
		o.MaxCommits = defaultMaxCommits
	}
	if o.Log == nil {
		o.Log = io.Discard
	}
	return o
}

// GitHistory is a Source that mines commit authors per changed path, so the
// people doing the work on a system surface even when nothing declares
// ownership. Authors join other sources by commit email.
type GitHistory struct {
	// opts holds the ingest configuration.
	opts GitOptions
}

// NewGitHistory returns a git history source over the given repositories.
func NewGitHistory(opts GitOptions) *GitHistory {
	return &GitHistory{opts: opts.withDefaults()}
}

// Fetch reads each repository's recent history and returns one record per
// author, weighted by how often they touched each topic.
func (g *GitHistory) Fetch(ctx context.Context) ([]Record, error) {
	if len(g.opts.Paths) == 0 {
		return nil, ErrNoRepoPaths
	}

	counts := make(map[string]map[string]int)
	names := make(map[string]string)
	latest := make(map[string]time.Time)
	for _, path := range g.opts.Paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		read, err := g.readRepo(path, counts, names, latest)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(g.opts.Log, "git: %s: %d commits\n", path, read)
	}

	records := make([]Record, 0, len(counts))
	for email, c := range counts {
		records = append(records, Record{
			Kind:   KindPerson,
			Source: "git",
			Weight: 1,
			Email:  email,
			Name:   names[email],
			Topics: expandTopics(c),
			Time:   latest[email],
		})
	}
	return records, nil
}

// readRepo walks one repository's log, accumulating per-author topic counts,
// display names, and latest activity. It returns the number of commits read.
func (g *GitHistory) readRepo(
	path string,
	counts map[string]map[string]int,
	names map[string]string,
	latest map[string]time.Time,
) (int, error) {
	repo, err := git.PlainOpen(path)
	if err != nil {
		return 0, fmt.Errorf("git: open %s: %w", path, err)
	}
	since := time.Now().AddDate(0, 0, -g.opts.SinceDays)
	iter, err := repo.Log(&git.LogOptions{Since: &since})
	if err != nil {
		return 0, fmt.Errorf("git: log %s: %w", path, err)
	}
	defer iter.Close()

	read := 0
	err = iter.ForEach(func(c *object.Commit) error {
		if read >= g.opts.MaxCommits {
			return storer.ErrStop
		}
		email := strings.ToLower(strings.TrimSpace(c.Author.Email))
		if email == "" || c.NumParents() > 1 || isBotAuthor(c.Author.Name, email) {
			return nil
		}
		stats, err := c.Stats()
		if err != nil {
			return nil
		}
		read++
		if c.Author.When.After(latest[email]) {
			latest[email] = c.Author.When
		}
		if c.Author.Name != "" {
			names[email] = c.Author.Name
		}
		m := counts[email]
		if m == nil {
			m = make(map[string]int)
			counts[email] = m
		}
		for _, stat := range stats {
			for _, tok := range pathTopics(stat.Name) {
				m[tok]++
			}
		}
		return nil
	})
	if err != nil {
		return read, fmt.Errorf("git: walk %s: %w", path, err)
	}
	return read, nil
}

// isBotAuthor reports whether a commit author is an automation account, such
// as dependabot, whose activity says nothing about human expertise.
func isBotAuthor(name, email string) bool {
	return strings.HasSuffix(name, "[bot]") || strings.Contains(email, "[bot]")
}
