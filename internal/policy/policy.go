// Package policy enforces data egress decisions. Every external call must pass
// through a Policy before any data leaves the machine. The default mode is
// Strict, which denies all egress.
package policy

import (
	"fmt"
	"strings"
)

// Mode is a data egress posture.
type Mode int

const (
	// Strict forbids all egress: nothing leaves the machine.
	Strict Mode = iota
	// Redacted permits egress only after the caller has redacted the payload.
	Redacted
	// Open permits egress without restriction.
	Open
)

// String returns the lowercase name of the mode.
func (m Mode) String() string {
	switch m {
	case Strict:
		return "strict"
	case Redacted:
		return "redacted"
	case Open:
		return "open"
	default:
		return "unknown"
	}
}

// ParseMode parses a mode name, defaulting to Strict on empty input.
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "strict":
		return Strict, nil
	case "redacted":
		return Redacted, nil
	case "open":
		return Open, nil
	default:
		return Strict, fmt.Errorf("%w: %q", ErrUnknownMode, s)
	}
}

// Policy decides whether data may leave the machine. A locked policy is pinned
// by an organization and cannot be loosened by user flags.
type Policy struct {
	// mode is the current egress posture.
	mode Mode
	// locked marks the policy as pinned and unoverridable when true.
	locked bool
	// privateOff, when true, forbids private-channel ingest regardless of flags.
	privateOff bool
}

// New returns a Policy with the given mode and lock state.
func New(mode Mode, locked bool) Policy {
	return Policy{mode: mode, locked: locked}
}

// Default returns the deny-all Strict policy.
func Default() Policy {
	return Policy{mode: Strict}
}

// Mode returns the policy's current mode.
func (p Policy) Mode() Mode { return p.mode }

// Locked reports whether the policy is pinned and cannot be loosened.
func (p Policy) Locked() bool { return p.locked }

// AllowPrivateChannels reports whether ingesting private channels is permitted.
// An organization can pin this off; user flags then cannot enable it.
func (p Policy) AllowPrivateChannels() bool { return !p.privateOff }

// WithoutPrivateChannels returns a copy that forbids private-channel ingest.
// This is how an organization pins private ingest off.
func (p Policy) WithoutPrivateChannels() Policy {
	c := p
	c.privateOff = true
	return c
}

// AllowEgress reports whether sending nbytes to dest is permitted. Strict always
// denies. Under Redacted and Open it permits; a Redacted caller is responsible
// for redacting the payload before calling.
func (p Policy) AllowEgress(dest string, nbytes int) error {
	switch p.mode {
	case Open, Redacted:
		return nil
	default:
		return fmt.Errorf("%w: mode=%s dest=%s bytes=%d", ErrEgressDenied, p.mode, dest, nbytes)
	}
}

// WithMode returns a copy at the requested mode. A locked policy cannot change
// to a different mode and returns ErrLocked.
func (p Policy) WithMode(mode Mode) (Policy, error) {
	if p.locked && mode != p.mode {
		return p, fmt.Errorf("%w: pinned at %s", ErrLocked, p.mode)
	}
	return Policy{mode: mode, locked: p.locked}, nil
}
