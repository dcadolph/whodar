package slack

import "errors"

// ErrRateLimited indicates Slack kept returning 429 past the retry budget.
var ErrRateLimited = errors.New("slack: rate limited")

// ErrAPI indicates Slack returned a logical error (ok=false).
var ErrAPI = errors.New("slack: api error")
