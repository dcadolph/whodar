package github

import (
	"context"
	"encoding/json"
	"net/url"
)

// Issue is the subset of an issue whodar reads. The GitHub issues endpoint also
// returns pull requests; IsPullRequest distinguishes them.
type Issue struct {
	// Title is the issue title.
	Title string `json:"title"`
	// User is the author.
	User account `json:"user"`
	// Assignees are users assigned to the issue.
	Assignees []account `json:"assignees"`
	// Labels are the applied labels.
	Labels []label `json:"labels"`
	// PullRequest is present when the issue is actually a pull request.
	PullRequest json.RawMessage `json:"pull_request"`
}

// IsPullRequest reports whether this issue is really a pull request.
func (i Issue) IsPullRequest() bool { return len(i.PullRequest) > 0 }

// Author returns the issue author's login.
func (i Issue) Author() string { return i.User.Login }

// LabelNames returns the label names.
func (i Issue) LabelNames() []string { return labelNames(i.Labels) }

// AssigneeLogins returns the assignees' logins.
func (i Issue) AssigneeLogins() []string { return accountLogins(i.Assignees) }

// Issues returns up to 100 recently updated issues of any state. The result
// includes pull requests, which the caller can filter with IsPullRequest.
func (c *Client) Issues(ctx context.Context, owner, repo string) ([]Issue, error) {
	var out []Issue
	q := url.Values{"state": {"all"}, "per_page": {"100"}, "sort": {"updated"}, "direction": {"desc"}}
	err := c.get(ctx, "/repos/"+owner+"/"+repo+"/issues", q, &out)
	return out, err
}
