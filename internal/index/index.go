// Package index builds an on-disk, searchable map of expertise from connector
// records and ranks people for a query without an LLM.
package index

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dcadolph/whodar/internal/connector"
	"github.com/dcadolph/whodar/internal/model"
)

// Field weights scale how strongly each signal contributes to a person's score.
// An explicit topic outweighs a title word, which outweighs a team word.
const (
	// weightTopic is the affinity weight of an explicit topic tag.
	weightTopic = 3.0
	// weightTitle is the affinity weight of a job-title word.
	weightTitle = 2.0
	// weightTeam is the affinity weight of a team-name word.
	weightTeam = 1.0
	// weightText is the affinity weight of a free-text word.
	weightText = 0.5
)

// personText holds the normalized, lowercased field text for a person, used to
// explain why the person matched a query.
type personText struct {
	// Title is the lowercased job title.
	Title string `json:"title"`
	// Team is the lowercased team name.
	Team string `json:"team"`
	// Topics are the lowercased explicit topic names.
	Topics []string `json:"topics"`
	// Text is the accumulated lowercased free text.
	Text string `json:"text"`
}

// Index is a searchable expertise index over a graph of people.
type Index struct {
	// Graph is the entity graph this index was built from.
	Graph *model.Graph
	// postings maps a search token to per-person accumulated weight.
	postings map[string]map[model.ID]float64
	// texts holds normalized per-person field text for reason lookup.
	texts map[model.ID]*personText
}

// New returns an empty index with initialized maps.
func New() *Index {
	return &Index{
		Graph:    model.NewGraph(),
		postings: make(map[string]map[model.ID]float64),
		texts:    make(map[model.ID]*personText),
	}
}

// Build replaces the index contents with data derived from records. Records for
// the same person, identified by email or name, are merged.
func (ix *Index) Build(records []connector.Record) {
	g := model.NewGraph()
	postings := make(map[string]map[model.ID]float64)
	texts := make(map[model.ID]*personText)

	for _, rec := range records {
		pid := personID(rec)
		if pid == "" {
			continue
		}
		w := rec.Weight
		if w == 0 {
			w = 1
		}

		p := g.People[pid]
		if p == nil {
			p = &model.Person{ID: pid, Topics: make(map[model.ID]float64)}
			g.People[pid] = p
		}
		fillIdentity(p, rec)
		linkOrg(g, p, rec)

		pt := texts[pid]
		if pt == nil {
			pt = &personText{}
			texts[pid] = pt
		}

		add := func(text string, fieldWeight float64) {
			for _, tok := range tokenize(text) {
				if postings[tok] == nil {
					postings[tok] = make(map[model.ID]float64)
				}
				postings[tok][pid] += fieldWeight * w
			}
		}
		if rec.Title != "" {
			pt.Title = strings.ToLower(rec.Title)
			add(rec.Title, weightTitle)
		}
		if rec.Team != "" {
			pt.Team = strings.ToLower(rec.Team)
			add(rec.Team, weightTeam)
		}
		for _, top := range rec.Topics {
			tid := topicID(top)
			if g.Topics[tid] == nil {
				g.Topics[tid] = &model.Topic{ID: tid, Name: strings.ToLower(top)}
			}
			p.Topics[tid] += weightTopic * w
			pt.Topics = append(pt.Topics, strings.ToLower(top))
			add(top, weightTopic)
		}
		if rec.Text != "" {
			pt.Text = strings.TrimSpace(pt.Text + " " + strings.ToLower(rec.Text))
			add(rec.Text, weightText)
		}
	}

	ix.Graph = g
	ix.postings = postings
	ix.texts = texts
}

// Search ranks people for query and returns up to limit matches. A non-positive
// limit returns all matches. Scoring sums per-term weights scaled by inverse
// document frequency, so rarer query terms count for more.
func (ix *Index) Search(query string, limit int) []model.Match {
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	n := float64(len(ix.Graph.People))
	scores := make(map[model.ID]float64)
	matched := make(map[model.ID]map[string]bool)

	for _, term := range terms {
		posting := ix.postings[term]
		if len(posting) == 0 {
			continue
		}
		idf := 1.0
		if n > 0 {
			idf = 1 + math.Log(n/float64(len(posting)))
		}
		for pid, w := range posting {
			scores[pid] += w * idf
			if matched[pid] == nil {
				matched[pid] = make(map[string]bool)
			}
			matched[pid][term] = true
		}
	}

	matches := make([]model.Match, 0, len(scores))
	for pid, sc := range scores {
		p := ix.Graph.People[pid]
		if p == nil {
			continue
		}
		var team *model.Team
		if p.TeamID != "" {
			team = ix.Graph.Teams[p.TeamID]
		}
		matches = append(matches, model.Match{
			Person:  p,
			Team:    team,
			Score:   sc,
			Reasons: ix.reasons(pid, matched[pid]),
		})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Person.ID < matches[j].Person.ID
	})
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

// reasons describes, for each matched term, which field of the person it hit.
func (ix *Index) reasons(pid model.ID, terms map[string]bool) []string {
	pt := ix.texts[pid]
	out := make([]string, 0, len(terms))
	for term := range terms {
		field := "mention"
		switch {
		case pt != nil && containsToken(pt.Topics, term):
			field = "topic"
		case pt != nil && strings.Contains(pt.Title, term):
			field = "title"
		case pt != nil && strings.Contains(pt.Team, term):
			field = "team"
		}
		out = append(out, fmt.Sprintf("%s (%s)", term, field))
	}
	sort.Strings(out)
	return out
}

// snapshot is the serializable form of an index written to and read from disk.
type snapshot struct {
	// Graph is the entity graph.
	Graph *model.Graph `json:"graph"`
	// Postings maps a token to per-person weight.
	Postings map[string]map[model.ID]float64 `json:"postings"`
	// Texts holds normalized per-person field text.
	Texts map[model.ID]*personText `json:"texts"`
}

// Save writes the index to path as JSON, creating parent directories as needed.
func (ix *Index) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("index: mkdir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("index: create: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(snapshot{Graph: ix.Graph, Postings: ix.postings, Texts: ix.texts}); err != nil {
		return fmt.Errorf("index: encode: %w", err)
	}
	return nil
}

// Load reads an index previously written by Save.
func Load(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("index: open: %w", err)
	}
	defer f.Close()

	var snap snapshot
	if err := json.NewDecoder(f).Decode(&snap); err != nil {
		return nil, fmt.Errorf("index: decode: %w", err)
	}
	ix := &Index{Graph: snap.Graph, postings: snap.Postings, texts: snap.Texts}
	if ix.Graph == nil {
		ix.Graph = model.NewGraph()
	}
	if ix.postings == nil {
		ix.postings = make(map[string]map[model.ID]float64)
	}
	if ix.texts == nil {
		ix.texts = make(map[model.ID]*personText)
	}
	return ix, nil
}

// personID resolves a stable identifier for a record, preferring an explicit
// id, then email, then a slug of the name.
func personID(rec connector.Record) model.ID {
	switch {
	case rec.PersonID != "":
		return model.ID(strings.ToLower(rec.PersonID))
	case rec.Email != "":
		return model.ID(strings.ToLower(rec.Email))
	case rec.Name != "":
		return model.ID(slug(rec.Name))
	default:
		return ""
	}
}

// topicID returns the identifier for a topic name.
func topicID(name string) model.ID {
	return model.ID(slug(name))
}

// fillIdentity copies non-empty identity fields from rec onto p.
func fillIdentity(p *model.Person, rec connector.Record) {
	if rec.Name != "" {
		p.Name = rec.Name
	}
	if rec.Email != "" {
		p.Email = rec.Email
	}
	if rec.Title != "" {
		p.Title = rec.Title
	}
	if rec.Manager != "" {
		p.ManagerID = model.ID(strings.ToLower(rec.Manager))
	}
}

// linkOrg attaches the person to their team and organization, creating those
// entities in the graph when first seen.
func linkOrg(g *model.Graph, p *model.Person, rec connector.Record) {
	var orgID model.ID
	if rec.Org != "" {
		orgID = model.ID(slug(rec.Org))
		if g.Orgs[orgID] == nil {
			g.Orgs[orgID] = &model.Org{ID: orgID, Name: rec.Org}
		}
		p.OrgID = orgID
	}
	if rec.Team != "" {
		teamID := model.ID(slug(rec.Team))
		if g.Teams[teamID] == nil {
			g.Teams[teamID] = &model.Team{ID: teamID, Name: rec.Team}
		}
		if orgID != "" {
			g.Teams[teamID].OrgID = orgID
		}
		p.TeamID = teamID
	}
}
