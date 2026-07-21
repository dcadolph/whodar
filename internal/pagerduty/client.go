// Package pagerduty is a minimal PagerDuty client scoped to what whodar
// ingests: services and who is on call for them. The token is held only in
// memory, never serialized, and never logged.
package pagerduty

import (
	"context"
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

// defaultBaseURL is the PagerDuty REST API root.
const defaultBaseURL = "https://api.pagerduty.com"

// ErrStatus indicates an unexpected HTTP status.
var ErrStatus = errors.New("pagerduty: unexpected status")

// ErrRateLimited indicates the API rate limit was exhausted.
var ErrRateLimited = errors.New("pagerduty: rate limited")

// Doer performs an HTTP request. *http.Client satisfies it; tests inject a stub.
type Doer interface {
	// Do performs the request and returns the response.
	Do(req *http.Request) (*http.Response, error)
}

// Client calls the PagerDuty REST API.
type Client struct {
	// token is the API token; it is never serialized or logged.
	token string
	// baseURL is the API root, overridable for tests.
	baseURL string
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

// WithBaseURL overrides the API base URL.
func WithBaseURL(u string) Option {
	return func(c *Client) {
		if u != "" {
			c.baseURL = u
		}
	}
}

// apiTimeout bounds one HTTP exchange so a hung server cannot stall a run.
const apiTimeout = 60 * time.Second

// New returns a Client for token. It panics on an empty token; callers validate
// token presence before constructing the client.
func New(token string, opts ...Option) *Client {
	if token == "" {
		panic("pagerduty: New requires a non-empty token")
	}
	c := &Client{token: token, baseURL: defaultBaseURL, http: &http.Client{Timeout: apiTimeout}, maxRetries: 3}
	for _, o := range opts {
		o(c)
	}
	return c
}

// EscalationPolicyRef references an escalation policy.
type EscalationPolicyRef struct {
	// ID is the escalation policy id.
	ID string `json:"id"`
}

// Service is a monitored service.
type Service struct {
	// ID is the service id.
	ID string `json:"id"`
	// Name is the service name.
	Name string `json:"name"`
	// Description is the service description.
	Description string `json:"description"`
	// EscalationPolicy links the service to its escalation policy.
	EscalationPolicy EscalationPolicyRef `json:"escalation_policy"`
}

// User is the subset of a PagerDuty user whodar reads.
type User struct {
	// ID is the user id.
	ID string `json:"id"`
	// Name is the user's display name.
	Name string `json:"name"`
	// Email is the user's email.
	Email string `json:"email"`
}

// OnCall is a current on-call assignment.
type OnCall struct {
	// User is the on-call user.
	User User `json:"user"`
	// EscalationPolicy is the policy the user is on call for.
	EscalationPolicy EscalationPolicyRef `json:"escalation_policy"`
}

// servicesResponse decodes the services endpoint.
type servicesResponse struct {
	// Services is the page of services.
	Services []Service `json:"services"`
	// More reports whether more pages remain.
	More bool `json:"more"`
}

// oncallsResponse decodes the oncalls endpoint.
type oncallsResponse struct {
	// OnCalls is the page of on-call assignments.
	OnCalls []OnCall `json:"oncalls"`
	// More reports whether more pages remain.
	More bool `json:"more"`
}

// Services returns all services with their escalation policy.
func (c *Client) Services(ctx context.Context) ([]Service, error) {
	var all []Service
	for offset := 0; ; offset += 100 {
		params := url.Values{"limit": {"100"}, "offset": {strconv.Itoa(offset)}}
		var resp servicesResponse
		if err := c.get(ctx, "/services", params, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Services...)
		if !resp.More {
			return all, nil
		}
	}
}

// OnCalls returns the current on-call assignments with the on-call users.
func (c *Client) OnCalls(ctx context.Context) ([]OnCall, error) {
	var all []OnCall
	for offset := 0; ; offset += 100 {
		params := url.Values{"limit": {"100"}, "offset": {strconv.Itoa(offset)}, "include[]": {"users"}}
		var resp oncallsResponse
		if err := c.get(ctx, "/oncalls", params, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.OnCalls...)
		if !resp.More {
			return all, nil
		}
	}
}

// get performs a GET request and decodes the JSON body into out, retrying on
// HTTP 429 up to maxRetries.
func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	endpoint := c.baseURL + path + "?" + params.Encode()
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return fmt.Errorf("pagerduty: new request: %w", err)
		}
		req.Header.Set("Authorization", "Token token="+c.token)
		req.Header.Set("Accept", "application/vnd.pagerduty+json;version=2")

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("pagerduty %s: %w", path, err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			wait := retryAfter(resp)
			_ = resp.Body.Close()
			if attempt >= c.maxRetries {
				return fmt.Errorf("pagerduty %s: %w", path, ErrRateLimited)
			}
			if err := sleep(ctx, wait); err != nil {
				return err
			}
			continue
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("pagerduty %s: read body: %w", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("pagerduty %s: %w %d", path, ErrStatus, resp.StatusCode)
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("pagerduty %s: decode: %w", path, err)
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
