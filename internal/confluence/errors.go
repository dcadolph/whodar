package confluence

import "errors"

// ErrStatus indicates an unexpected HTTP status.
var ErrStatus = errors.New("confluence: unexpected status")

// ErrRateLimited indicates the API rate limit was exhausted.
var ErrRateLimited = errors.New("confluence: rate limited")
