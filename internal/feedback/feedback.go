// Package feedback stores user votes on answers so confirmed answers rise and
// corrected ones sink in future rankings. Votes live in their own file, apart
// from the index, so they survive re-indexing.
package feedback

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dcadolph/whodar/internal/util"
)

// Votes a user can cast on one result.
const (
	// Helpful marks a result the user confirmed.
	Helpful = 1
	// NotHelpful marks a result the user corrected.
	NotHelpful = -1
)

// Entry is one vote on one result for one question.
type Entry struct {
	// Query is the question as asked.
	Query string `json:"query"`
	// Person is the voted person's identifier, when the vote is on a person.
	Person string `json:"person,omitempty"`
	// Channel is the voted channel's identifier, when the vote is on a channel.
	Channel string `json:"channel,omitempty"`
	// Vote is Helpful or NotHelpful.
	Vote int `json:"vote"`
	// Comment is an optional note explaining the vote.
	Comment string `json:"comment,omitempty"`
	// Time is when the vote was cast.
	Time time.Time `json:"time"`
}

// Valid reports whether the entry names a query, exactly one target, and a
// known vote.
func (e Entry) Valid() bool {
	if e.Query == "" || (e.Vote != Helpful && e.Vote != NotHelpful) {
		return false
	}
	return (e.Person != "") != (e.Channel != "")
}

// Store holds votes and persists them as JSON. It is safe for concurrent use.
type Store struct {
	// mu guards entries.
	mu sync.Mutex
	// entries are the recorded votes, oldest first.
	entries []Entry
	// path is the file the store persists to.
	path string
}

// Load reads a store from path. A missing file yields an empty store.
func Load(path string) (*Store, error) {
	s := &Store{path: path}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("feedback: read %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &s.entries); err != nil {
		return nil, fmt.Errorf("feedback: parse %s: %w", path, err)
	}
	return s, nil
}

// Add records a vote and persists the store.
func (s *Store) Add(e Entry) error {
	if !e.Valid() {
		return fmt.Errorf("feedback: %w", ErrBadEntry)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
	return s.save()
}

// All returns a copy of the recorded votes.
func (s *Store) All() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Entry(nil), s.entries...)
}

// Filter selects the votes to list or clear. Zero fields match everything;
// set fields must all match.
type Filter struct {
	// Query matches entries for this exact question, case-insensitively.
	Query string
	// Person matches votes on this person identifier.
	Person string
	// Channel matches votes on this channel name.
	Channel string
}

// matches reports whether e satisfies every set field of f.
func (f Filter) matches(e Entry) bool {
	if f.Query != "" && !strings.EqualFold(f.Query, e.Query) {
		return false
	}
	if f.Person != "" && !strings.EqualFold(f.Person, e.Person) {
		return false
	}
	if f.Channel != "" && !strings.EqualFold(f.Channel, e.Channel) {
		return false
	}
	return true
}

// List returns the votes matching f, oldest first.
func (s *Store) List(f Filter) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Entry
	for _, e := range s.entries {
		if f.matches(e) {
			out = append(out, e)
		}
	}
	return out
}

// Clear removes the votes matching f, persists the store, and returns how
// many were removed.
func (s *Store) Clear(f Filter) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.entries[:0]
	removed := 0
	for _, e := range s.entries {
		if f.matches(e) {
			removed++
			continue
		}
		kept = append(kept, e)
	}
	s.entries = kept
	if removed == 0 {
		return 0, nil
	}
	return removed, s.save()
}

// save writes the entries to the store's path atomically. Callers hold the
// lock.
func (s *Store) save() error {
	raw, err := json.Marshal(s.entries)
	if err != nil {
		return fmt.Errorf("feedback: encode: %w", err)
	}
	if err := util.WriteFileAtomic(s.path, raw, 0o600); err != nil {
		return fmt.Errorf("feedback: write %s: %w", s.path, err)
	}
	return nil
}
