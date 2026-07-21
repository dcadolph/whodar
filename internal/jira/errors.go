package jira

import "errors"

// ErrStatus indicates an unexpected HTTP status.
var ErrStatus = errors.New("jira: unexpected status")

// ErrRateLimited indicates the API rate limit was exhausted.
var ErrRateLimited = errors.New("jira: rate limited")
