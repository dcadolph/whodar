// Package confluence is a minimal Confluence Cloud client scoped to what whodar
// ingests: pages and the people who wrote them. Credentials are held only in
// memory, never serialized, and never logged.
package confluence

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

// searchPath is the Confluence Cloud content search endpoint.
const searchPath = "/wiki/rest/api/content/search"

// ErrStatus indicates an unexpected HTTP status.
var ErrStatus = errors.New("confluence: unexpected status")

// ErrRateLimited indicates the API rate limit was exhausted.
var ErrRateLimited = errors.New("confluence: rate limited")

// Doer performs an HTTP request. *http.Client satisfies it; tests inject a stub.
type Doer interface {
	// Do performs the request and returns the response.
	Do(req *http.Request) (*http.Response, error)
}

// Client calls the Confluence Cloud REST API.
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
func New(siteURL, email, token string, opts ...Option) *Client {
	if siteURL == "" || email == "" || token == "" {
		panic("confluence: New requires siteURL, email, and token")
	}
	c := &Client{
		baseURL:    strings.TrimRight(siteURL, "/"),
		auth:       "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+token)),
		http:       http.DefaultClient,
		maxRetries: 3,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// User is the subset of a Confluence user whodar reads.
type User struct {
	// AccountID is the stable account identifier.
	AccountID string `json:"accountId"`
	// DisplayName is the user's display name.
	DisplayName string `json:"displayName"`
	// Email is the user's email, present when visible to the token.
	Email string `json:"email"`
}

// label is a content label.
type label struct {
	// Name is the label text.
	Name string `json:"name"`
}

// Page is the subset of a content page whodar reads.
type Page struct {
	// Title is the page title.
	Title string `json:"title"`
	// Space is the page's space.
	Space struct {
		// Key is the space key.
		Key string `json:"key"`
		// Name is the space name.
		Name string `json:"name"`
	} `json:"space"`
	// Metadata holds the page labels.
	Metadata struct {
		// Labels are the page labels.
		Labels struct {
			// Results is the label list.
			Results []label `json:"results"`
		} `json:"labels"`
	} `json:"metadata"`
	// History holds the page creator.
	History struct {
		// CreatedBy is the page's creator.
		CreatedBy *User `json:"createdBy"`
	} `json:"history"`
	// Version holds the last editor.
	Version struct {
		// By is the last editor.
		By *User `json:"by"`
	} `json:"version"`
}

// LabelNames returns the page's label names.
func (p Page) LabelNames() []string {
	out := make([]string, 0, len(p.Metadata.Labels.Results))
	for _, l := range p.Metadata.Labels.Results {
		out = append(out, l.Name)
	}
	return out
}

// Authors returns the distinct creator and last editor of the page.
func (p Page) Authors() []*User {
	var out []*User
	seen := make(map[string]bool)
	for _, u := range []*User{p.History.CreatedBy, p.Version.By} {
		if u != nil && u.AccountID != "" && !seen[u.AccountID] {
			seen[u.AccountID] = true
			out = append(out, u)
		}
	}
	return out
}

// searchResponse decodes the content search endpoint.
type searchResponse struct {
	// Results is the page of content.
	Results []Page `json:"results"`
	// Size is the number returned in this page.
	Size int `json:"size"`
	// Limit is the requested page size.
	Limit int `json:"limit"`
}

// Pages returns up to max pages matching cql, paginating in pages of 100. A
// non-positive max returns all matches.
func (c *Client) Pages(ctx context.Context, cql string, max int) ([]Page, error) {
	var all []Page
	for start := 0; ; {
		limit := 100
		if max > 0 && max-len(all) < limit {
			limit = max - len(all)
		}
		if limit <= 0 {
			break
		}
		params := url.Values{
			"cql":    {cql},
			"start":  {strconv.Itoa(start)},
			"limit":  {strconv.Itoa(limit)},
			"expand": {"space,metadata.labels,history,version"},
		}
		var resp searchResponse
		if err := c.get(ctx, searchPath, params, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Results...)
		start += resp.Size
		if resp.Size == 0 || resp.Size < limit {
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
			return fmt.Errorf("confluence: new request: %w", err)
		}
		req.Header.Set("Authorization", c.auth)
		req.Header.Set("Accept", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("confluence %s: %w", path, err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			wait := retryAfter(resp)
			resp.Body.Close()
			if attempt >= c.maxRetries {
				return fmt.Errorf("confluence %s: %w", path, ErrRateLimited)
			}
			if err := sleep(ctx, wait); err != nil {
				return err
			}
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("confluence %s: read body: %w", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("confluence %s: %w %d", path, ErrStatus, resp.StatusCode)
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("confluence %s: decode: %w", path, err)
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
