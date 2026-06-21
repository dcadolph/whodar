// Package connector pulls raw records from work sources and normalizes them
// into records the indexer merges into the expertise graph.
package connector

import "context"

// Record is one normalized observation about a person from a source: identity,
// org placement, explicit topics, and free text mined for further topics.
type Record struct {
	// PersonID is a stable per-source identifier; empty derives one from email.
	PersonID string
	// Name is the person's display name.
	Name string
	// Email is the person's work email.
	Email string
	// Title is the person's job title.
	Title string
	// Team is the person's team name.
	Team string
	// Org is the person's organization name.
	Org string
	// Manager is the manager's email or identifier, if known.
	Manager string
	// Topics are explicit expertise tags for the person.
	Topics []string
	// Text is free-form text attributed to the person, mined for topics.
	Text string
	// Source names the origin connector, e.g. "org-csv".
	Source string
	// Weight scales this record's affinity contribution; zero means one.
	Weight float64
}

// Source fetches and normalizes records from one origin.
type Source interface {
	// Fetch returns the records this source currently provides.
	Fetch(ctx context.Context) ([]Record, error)
}

// SourceFunc adapts a function to the Source interface.
type SourceFunc func(ctx context.Context) ([]Record, error)

// Fetch calls f.
func (f SourceFunc) Fetch(ctx context.Context) ([]Record, error) { return f(ctx) }
