// Package github is a minimal GitHub REST client scoped to what whodar ingests:
// repositories, contributors, pull requests, CODEOWNERS, and users. The token
// is held only in memory, never serialized, and never logged.
package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// defaultBaseURL is the GitHub REST API root.
const defaultBaseURL = "https://api.github.com"

// ErrNotFound indicates the requested resource does not exist.
var ErrNotFound = errors.New("github: not found")

// ErrRateLimited indicates the API rate limit was exhausted.
var ErrRateLimited = errors.New("github: rate limited")

// ErrStatus indicates an unexpected HTTP status.
var ErrStatus = errors.New("github: unexpected status")

// Doer performs an HTTP request. *http.Client satisfies it; tests inject a stub.
type Doer interface {
	// Do performs the request and returns the response.
	Do(req *http.Request) (*http.Response, error)
}

// Client calls the GitHub REST API.
type Client struct {
	// token is the bearer token; it is never serialized or logged.
	token string
	// baseURL is the API root, overridable for tests.
	baseURL string
	// http performs requests.
	http Doer
	// maxRetries bounds retries on a secondary rate limit.
	maxRetries int
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets the HTTP doer.
func WithHTTPClient(d Doer) Option {
	return func(c *Client) {
		if d != nil {
			c.http = d
		}
	}
}

// WithBaseURL overrides the API base URL.
func WithBaseURL(u string) Option {
	return func(c *Client) {
		if u != "" {
			c.baseURL = u
		}
	}
}

// New returns a Client for token. It panics on an empty token; callers validate
// token presence before constructing the client.
func New(token string, opts ...Option) *Client {
	if token == "" {
		panic("github: New requires a non-empty token")
	}
	c := &Client{token: token, baseURL: defaultBaseURL, http: http.DefaultClient, maxRetries: 3}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Repo is a repository's indexable metadata.
type Repo struct {
	// Name is the repository name.
	Name string `json:"name"`
	// FullName is the owner/name path.
	FullName string `json:"full_name"`
	// Description is the repository description.
	Description string `json:"description"`
	// Topics are the repository's topic tags.
	Topics []string `json:"topics"`
}

// Contributor is a repository contributor.
type Contributor struct {
	// Login is the contributor's handle.
	Login string `json:"login"`
	// Contributions is the contributor's commit count.
	Contributions int `json:"contributions"`
}

// label is a pull request label.
type label struct {
	// Name is the label text.
	Name string `json:"name"`
}

// account is a user reference inside other payloads.
type account struct {
	// Login is the user's handle.
	Login string `json:"login"`
}

// PullRequest is the subset of a pull request whodar reads.
type PullRequest struct {
	// Title is the pull request title.
	Title string `json:"title"`
	// User is the author.
	User account `json:"user"`
	// Labels are the applied labels.
	Labels []label `json:"labels"`
	// RequestedReviewers are users asked to review.
	RequestedReviewers []account `json:"requested_reviewers"`
}

// Author returns the pull request author's login.
func (p PullRequest) Author() string { return p.User.Login }

// LabelNames returns the label names.
func (p PullRequest) LabelNames() []string {
	out := make([]string, 0, len(p.Labels))
	for _, l := range p.Labels {
		out = append(out, l.Name)
	}
	return out
}

// Reviewers returns the requested reviewers' logins.
func (p PullRequest) Reviewers() []string {
	out := make([]string, 0, len(p.RequestedReviewers))
	for _, r := range p.RequestedReviewers {
		out = append(out, r.Login)
	}
	return out
}

// Account is a user's public profile subset.
type Account struct {
	// Login is the user's handle.
	Login string `json:"login"`
	// Name is the user's display name.
	Name string `json:"name"`
	// Email is the user's public email, often empty.
	Email string `json:"email"`
}

// Repo returns a repository's metadata.
func (c *Client) Repo(ctx context.Context, owner, repo string) (Repo, error) {
	var r Repo
	err := c.get(ctx, "/repos/"+owner+"/"+repo, nil, &r)
	return r, err
}

// Contributors returns up to 100 contributors, most commits first.
func (c *Client) Contributors(ctx context.Context, owner, repo string) ([]Contributor, error) {
	var out []Contributor
	err := c.get(ctx, "/repos/"+owner+"/"+repo+"/contributors", url.Values{"per_page": {"100"}}, &out)
	return out, err
}

// PullRequests returns up to 100 recently updated pull requests of any state.
func (c *Client) PullRequests(ctx context.Context, owner, repo string) ([]PullRequest, error) {
	var out []PullRequest
	q := url.Values{"state": {"all"}, "per_page": {"100"}, "sort": {"updated"}, "direction": {"desc"}}
	err := c.get(ctx, "/repos/"+owner+"/"+repo+"/pulls", q, &out)
	return out, err
}

// OrgRepos returns up to 100 recently updated repositories in an org.
func (c *Client) OrgRepos(ctx context.Context, org string) ([]Repo, error) {
	var out []Repo
	err := c.get(ctx, "/orgs/"+org+"/repos", url.Values{"per_page": {"100"}, "sort": {"updated"}}, &out)
	return out, err
}

// Account returns a user's public profile.
func (c *Client) Account(ctx context.Context, login string) (Account, error) {
	var a Account
	err := c.get(ctx, "/users/"+login, nil, &a)
	return a, err
}

// FileContents returns the decoded contents of a file, or ErrNotFound if it is
// absent.
func (c *Client) FileContents(ctx context.Context, owner, repo, path string) ([]byte, error) {
	var resp struct {
		// Content is the base64-encoded file content.
		Content string `json:"content"`
		// Encoding is the content encoding, normally base64.
		Encoding string `json:"encoding"`
	}
	if err := c.get(ctx, "/repos/"+owner+"/"+repo+"/contents/"+path, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Encoding != "base64" {
		return []byte(resp.Content), nil
	}
	return base64.StdEncoding.DecodeString(strings.ReplaceAll(resp.Content, "\n", ""))
}

// get performs a GET request and decodes the JSON body into out. It retries on a
// secondary rate limit that sends Retry-After, and returns ErrRateLimited or
// ErrNotFound for those conditions.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return fmt.Errorf("github: new request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("github %s: %w", path, err)
		}

		if wait, ok := retryAfter(resp); ok {
			resp.Body.Close()
			if attempt >= c.maxRetries {
				return fmt.Errorf("github %s: %w", path, ErrRateLimited)
			}
			if err := sleep(ctx, wait); err != nil {
				return err
			}
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("github %s: read body: %w", path, err)
		}
		switch {
		case resp.StatusCode == http.StatusNotFound:
			return fmt.Errorf("github %s: %w", path, ErrNotFound)
		case resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
			return fmt.Errorf("github %s: %w (resets at %s)", path, ErrRateLimited, resetTime(resp))
		case resp.StatusCode != http.StatusOK:
			return fmt.Errorf("github %s: %w %d", path, ErrStatus, resp.StatusCode)
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("github %s: decode: %w", path, err)
		}
		return nil
	}
}

// retryAfter reports a secondary rate-limit wait when the response carries a
// Retry-After header.
func retryAfter(resp *http.Response) (time.Duration, bool) {
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	return 0, false
}

// resetTime formats the X-RateLimit-Reset header as a time, or "unknown".
func resetTime(resp *http.Response) string {
	v := resp.Header.Get("X-RateLimit-Reset")
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Unix(secs, 0).UTC().Format(time.RFC3339)
	}
	return "unknown"
}

// sleep waits for d or until ctx is canceled.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
