// Package jira is a minimal Jira Cloud client scoped to what whodar ingests:
// issues and the people on them. Credentials are held only in memory, never
// serialized, and never logged.
package jira

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

// searchPath is the Jira Cloud issue search endpoint.
const searchPath = "/rest/api/3/search"

// ErrStatus indicates an unexpected HTTP status.
var ErrStatus = errors.New("jira: unexpected status")

// ErrRateLimited indicates the API rate limit was exhausted.
var ErrRateLimited = errors.New("jira: rate limited")

// Doer performs an HTTP request. *http.Client satisfies it; tests inject a stub.
type Doer interface {
	// Do performs the request and returns the response.
	Do(req *http.Request) (*http.Response, error)
}

// Client calls the Jira Cloud REST API.
type Client struct {
	// baseURL is the site root, for example https://acme.atlassian.net.
	baseURL string
	// auth is the Basic authorization header value.
	auth string
	// http performs requests.
	http Doer
	// maxRetries bounds retries on HTTP 429.
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

// New returns a Client for the site, authenticating with an email and API
// token. It panics if any argument is empty.
func New(baseURL, email, token string, opts ...Option) *Client {
	if baseURL == "" || email == "" || token == "" {
		panic("jira: New requires baseURL, email, and token")
	}
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		auth:       "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+token)),
		http:       http.DefaultClient,
		maxRetries: 3,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// User is the subset of a Jira user whodar reads.
type User struct {
	// AccountID is the stable account identifier.
	AccountID string `json:"accountId"`
	// DisplayName is the user's display name.
	DisplayName string `json:"displayName"`
	// EmailAddress is the user's email, present when visible to the token.
	EmailAddress string `json:"emailAddress"`
}

// Component is an issue component.
type Component struct {
	// Name is the component name.
	Name string `json:"name"`
}

// Issue is the subset of an issue whodar reads.
type Issue struct {
	// Key is the issue key, for example PROJ-12.
	Key string `json:"key"`
	// Fields holds the issue fields.
	Fields struct {
		// Summary is the issue title.
		Summary string `json:"summary"`
		// Assignee is the assigned user, if any.
		Assignee *User `json:"assignee"`
		// Reporter is the reporting user, if any.
		Reporter *User `json:"reporter"`
		// Components are the issue components.
		Components []Component `json:"components"`
		// Labels are the issue labels.
		Labels []string `json:"labels"`
		// Project is the issue's project.
		Project struct {
			// Key is the project key.
			Key string `json:"key"`
			// Name is the project name.
			Name string `json:"name"`
		} `json:"project"`
		// IssueType is the issue type.
		IssueType struct {
			// Name is the issue type name.
			Name string `json:"name"`
		} `json:"issuetype"`
	} `json:"fields"`
}

// searchResponse decodes the issue search endpoint.
type searchResponse struct {
	// Issues is the page of issues.
	Issues []Issue `json:"issues"`
	// StartAt is the offset of this page.
	StartAt int `json:"startAt"`
	// Total is the total matching count.
	Total int `json:"total"`
}

// Search returns up to max issues matching jql, paginating in pages of 100. A
// non-positive max returns all matches.
func (c *Client) Search(ctx context.Context, jql string, max int) ([]Issue, error) {
	var all []Issue
	for startAt := 0; ; {
		page := 100
		if max > 0 && max-len(all) < page {
			page = max - len(all)
		}
		if page <= 0 {
			break
		}
		params := url.Values{
			"jql":        {jql},
			"startAt":    {strconv.Itoa(startAt)},
			"maxResults": {strconv.Itoa(page)},
			"fields":     {"summary,assignee,reporter,components,labels,project,issuetype"},
		}
		var resp searchResponse
		if err := c.get(ctx, searchPath, params, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Issues...)
		startAt += len(resp.Issues)
		if len(resp.Issues) == 0 || startAt >= resp.Total {
			break
		}
		if max > 0 && len(all) >= max {
			break
		}
	}
	return all, nil
}

// get performs a GET request and decodes the JSON body into out, retrying on
// HTTP 429 up to maxRetries.
func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	endpoint := c.baseURL + path + "?" + params.Encode()
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return fmt.Errorf("jira: new request: %w", err)
		}
		req.Header.Set("Authorization", c.auth)
		req.Header.Set("Accept", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("jira %s: %w", path, err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			wait := retryAfter(resp)
			_ = resp.Body.Close()
			if attempt >= c.maxRetries {
				return fmt.Errorf("jira %s: %w", path, ErrRateLimited)
			}
			if err := sleep(ctx, wait); err != nil {
				return err
			}
			continue
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("jira %s: read body: %w", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("jira %s: %w %d", path, ErrStatus, resp.StatusCode)
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("jira %s: decode: %w", path, err)
		}
		return nil
	}
}

// retryAfter reads the Retry-After header in seconds, defaulting to one second.
func retryAfter(resp *http.Response) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return time.Second
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
