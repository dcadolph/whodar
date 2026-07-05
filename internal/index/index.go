// Package index builds an on-disk, searchable map of expertise from connector
// records and ranks people and channels for a query without an LLM.
package index

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/dcadolph/whodar/internal/connector"
	"github.com/dcadolph/whodar/internal/identity"
	"github.com/dcadolph/whodar/internal/model"
)

// DefaultHalfLife is the age at which a dated record's weight halves.
const DefaultHalfLife = 180 * 24 * time.Hour

// Field weights scale how strongly each signal contributes to a score. An
// explicit topic or a channel name outweighs a title word, which outweighs a
// team word, which outweighs free text.
const (
	// weightTopic is the affinity weight of an explicit topic tag.
	weightTopic = 3.0
	// weightChannelName is the affinity weight of the channel's own name.
	weightChannelName = 3.0
	// weightTitle is the affinity weight of a job-title word.
	weightTitle = 2.0
	// weightTeam is the affinity weight of a team-name word.
	weightTeam = 1.0
	// weightText is the affinity weight of a free-text word.
	weightText = 0.5
)

// personText holds the normalized field text for a person, used to explain why
// the person matched a query.
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

// channelText holds the normalized field text for a channel, used to explain
// why the channel matched a query.
type channelText struct {
	// Name is the lowercased channel name.
	Name string `json:"name"`
	// Topic is the lowercased channel topic.
	Topic string `json:"topic"`
	// Topics are the lowercased explicit topic names.
	Topics []string `json:"topics"`
}

// Index is a searchable expertise index over people and channels.
type Index struct {
	// Graph is the entity graph this index was built from.
	Graph *model.Graph
	// postings maps a token to per-person accumulated weight.
	postings map[string]map[model.ID]float64
	// texts holds normalized per-person field text for reason lookup.
	texts map[model.ID]*personText
	// channelPostings maps a token to per-channel accumulated weight.
	channelPostings map[string]map[model.ID]float64
	// channelTexts holds normalized per-channel field text for reason lookup.
	channelTexts map[model.ID]*channelText
	// personVecs holds per-person embedding vectors when present.
	personVecs map[model.ID][]float32
	// channelVecs holds per-channel embedding vectors when present.
	channelVecs map[model.ID][]float32
	// resolver maps the identifiers a person accumulates across sources to one
	// canonical identifier.
	resolver *identity.Resolver
	// halfLife is the age at which a dated record's weight halves; zero or
	// negative disables recency decay.
	halfLife time.Duration
	// now returns the current time; tests pin it for deterministic decay.
	now func() time.Time
}

// New returns an empty index with initialized maps.
func New() *Index {
	return &Index{
		Graph:           model.NewGraph(),
		postings:        make(map[string]map[model.ID]float64),
		texts:           make(map[model.ID]*personText),
		channelPostings: make(map[string]map[model.ID]float64),
		channelTexts:    make(map[model.ID]*channelText),
		personVecs:      make(map[model.ID][]float32),
		channelVecs:     make(map[model.ID][]float32),
		resolver:        identity.NewResolver(),
		halfLife:        DefaultHalfLife,
		now:             time.Now,
	}
}

// SetHalfLife sets the age at which a dated record's weight halves. Zero or
// negative disables recency decay.
func (ix *Index) SetHalfLife(d time.Duration) { ix.halfLife = d }

// decay returns the recency multiplier for a record dated t: one for undated
// records, future dates, or disabled decay, halving per half-life of age.
func (ix *Index) decay(t time.Time) float64 {
	if t.IsZero() || ix.halfLife <= 0 {
		return 1
	}
	age := ix.now().Sub(t)
	if age <= 0 {
		return 1
	}
	return math.Exp2(-float64(age) / float64(ix.halfLife))
}

// Build replaces the index contents with data derived from records.
func (ix *Index) Build(records []connector.Record) {
	ix.Graph = model.NewGraph()
	ix.postings = make(map[string]map[model.ID]float64)
	ix.texts = make(map[model.ID]*personText)
	ix.channelPostings = make(map[string]map[model.ID]float64)
	ix.channelTexts = make(map[model.ID]*channelText)
	ix.personVecs = make(map[model.ID][]float32)
	ix.channelVecs = make(map[model.ID][]float32)
	ix.Add(records)
}

// Add merges records into the current index, accumulating onto whatever is
// already present. Person records merge by email or id, channel records by
// name. It leaves embeddings unchanged; call Embed to refresh vectors after
// adding.
func (ix *Index) Add(records []connector.Record) {
	for _, rec := range records {
		switch rec.Kind {
		case connector.KindChannel:
			ix.buildChannel(rec)
		default:
			ix.buildPerson(rec)
		}
	}
}

// LoadAliases merges a JSON alias file into the index's identity resolver so
// records indexed afterward key by their canonical identifier. Call
// Canonicalize to also join people already in the graph.
func (ix *Index) LoadAliases(path string) error {
	return ix.identityResolver().LoadFile(path)
}

// Alias records that two identifiers belong to the same person. Records
// indexed afterward key by the canonical identifier; call Canonicalize to
// also join people already in the graph.
func (ix *Index) Alias(a, b model.ID) {
	ix.identityResolver().Union(a, b)
}

// identityResolver returns the index's resolver, initializing it on first use.
func (ix *Index) identityResolver() *identity.Resolver {
	if ix.resolver == nil {
		ix.resolver = identity.NewResolver()
	}
	return ix.resolver
}

// buildPerson merges one person record into the graph and postings.
func (ix *Index) buildPerson(rec connector.Record) {
	g, postings, texts, r := ix.Graph, ix.postings, ix.texts, ix.identityResolver()
	raw := personID(rec)
	if raw == "" {
		return
	}
	if rec.PersonID != "" && rec.Email != "" {
		r.Union(model.ID(rec.Email), model.ID(rec.PersonID))
	}
	pid := r.Canonical(raw)
	w := rec.Weight
	if w == 0 {
		w = 1
	}
	w *= ix.decay(rec.Time)
	p := g.People[pid]
	if p == nil {
		p = &model.Person{ID: pid, Topics: make(map[model.ID]float64)}
		g.People[pid] = p
	}
	if raw != pid && !slices.Contains(p.Identities, raw) {
		p.Identities = append(p.Identities, raw)
	}
	fillIdentity(p, rec)
	if p.ManagerID != "" {
		p.ManagerID = r.Canonical(p.ManagerID)
	}
	linkOrg(g, p, rec)

	pt := texts[pid]
	if pt == nil {
		pt = &personText{}
		texts[pid] = pt
	}
	add := func(text string, fieldWeight float64) {
		for _, tok := range tokenize(text) {
			key := stem(tok)
			if postings[key] == nil {
				postings[key] = make(map[model.ID]float64)
			}
			postings[key][pid] += fieldWeight * w
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

// buildChannel merges one channel record into the graph and channel postings.
func (ix *Index) buildChannel(rec connector.Record) {
	g, postings, texts, r := ix.Graph, ix.channelPostings, ix.channelTexts, ix.identityResolver()
	cid := model.ID(slug(rec.Name))
	if cid == "" {
		return
	}
	d := ix.decay(rec.Time)
	ch := g.Channels[cid]
	if ch == nil {
		ch = &model.Channel{ID: cid, Name: rec.Name, Topics: make(map[model.ID]float64)}
		g.Channels[cid] = ch
	}
	if rec.Title != "" {
		ch.Topic = rec.Title
	}
	for _, m := range rec.Members {
		mid := r.Canonical(model.ID(strings.ToLower(m)))
		if !slices.Contains(ch.Members, mid) {
			ch.Members = append(ch.Members, mid)
		}
	}

	ct := texts[cid]
	if ct == nil {
		ct = &channelText{Name: strings.ToLower(rec.Name)}
		texts[cid] = ct
	}
	if rec.Title != "" {
		ct.Topic = strings.ToLower(rec.Title)
	}
	add := func(text string, fieldWeight float64) {
		for _, tok := range tokenize(text) {
			key := stem(tok)
			if postings[key] == nil {
				postings[key] = make(map[model.ID]float64)
			}
			postings[key][cid] += fieldWeight * d
		}
	}
	add(rec.Name, weightChannelName)
	if rec.Title != "" {
		add(rec.Title, weightTopic)
	}
	for _, top := range rec.Topics {
		tid := topicID(top)
		if g.Topics[tid] == nil {
			g.Topics[tid] = &model.Topic{ID: tid, Name: strings.ToLower(top)}
		}
		ch.Topics[tid] += weightTopic * d
		ct.Topics = append(ct.Topics, strings.ToLower(top))
		add(top, weightTopic)
	}
	if rec.Text != "" {
		add(rec.Text, weightText)
	}
}

// Search ranks people for query and returns up to limit matches. A non-positive
// limit returns all matches.
func (ix *Index) Search(query string, limit int) []model.Match {
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	scores, matched := scoreByTerms(ix.postings, terms, len(ix.Graph.People))

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

// SearchChannels ranks channels for query and returns up to limit matches, each
// carrying the most relevant active members. A non-positive limit returns all.
func (ix *Index) SearchChannels(query string, limit int) []model.ChannelMatch {
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	scores, matched := scoreByTerms(ix.channelPostings, terms, len(ix.Graph.Channels))
	personScores, _ := scoreByTerms(ix.postings, terms, len(ix.Graph.People))

	matches := make([]model.ChannelMatch, 0, len(scores))
	for cid, sc := range scores {
		ch := ix.Graph.Channels[cid]
		if ch == nil {
			continue
		}
		matches = append(matches, model.ChannelMatch{
			Channel:    ch,
			Score:      sc,
			Reasons:    ix.channelReasons(cid, matched[cid]),
			TopMembers: ix.topMembers(ch, personScores, 3),
		})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Channel.ID < matches[j].Channel.ID
	})
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

// topMembers returns up to n of a channel's members, most relevant to the query
// first, using precomputed person scores.
func (ix *Index) topMembers(ch *model.Channel, scores map[model.ID]float64, n int) []*model.Person {
	ids := append([]model.ID(nil), ch.Members...)
	sort.SliceStable(ids, func(i, j int) bool {
		return scores[ids[i]] > scores[ids[j]]
	})
	out := make([]*model.Person, 0, n)
	for _, id := range ids {
		if p := ix.Graph.People[id]; p != nil {
			out = append(out, p)
			if len(out) >= n {
				break
			}
		}
	}
	return out
}

// scoreByTerms accumulates per-entity scores over terms, weighting each term by
// inverse document frequency so rarer terms count for more. It returns the
// scores and, per entity, the set of terms that matched.
func scoreByTerms(
	postings map[string]map[model.ID]float64,
	terms []string,
	universe int,
) (map[model.ID]float64, map[model.ID]map[string]bool) {
	scores := make(map[model.ID]float64)
	matched := make(map[model.ID]map[string]bool)
	for _, term := range terms {
		posting := postings[stem(term)]
		if len(posting) == 0 {
			continue
		}
		idf := 1.0
		if universe > 0 {
			idf = 1 + math.Log(float64(universe)/float64(len(posting)))
		}
		for id, w := range posting {
			scores[id] += w * idf
			if matched[id] == nil {
				matched[id] = make(map[string]bool)
			}
			matched[id][term] = true
		}
	}
	return scores, matched
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

// channelReasons describes, for each matched term, which field of the channel
// it hit.
func (ix *Index) channelReasons(cid model.ID, terms map[string]bool) []string {
	ct := ix.channelTexts[cid]
	out := make([]string, 0, len(terms))
	for term := range terms {
		field := "mention"
		switch {
		case ct != nil && containsToken(ct.Topics, term):
			field = "topic"
		case ct != nil && strings.Contains(ct.Topic, term):
			field = "topic"
		case ct != nil && strings.Contains(ct.Name, term):
			field = "name"
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
	// ChannelPostings maps a token to per-channel weight.
	ChannelPostings map[string]map[model.ID]float64 `json:"channel_postings"`
	// ChannelTexts holds normalized per-channel field text.
	ChannelTexts map[model.ID]*channelText `json:"channel_texts"`
	// PersonVecs holds per-person embedding vectors.
	PersonVecs map[model.ID][]float32 `json:"person_vecs,omitempty"`
	// ChannelVecs holds per-channel embedding vectors.
	ChannelVecs map[model.ID][]float32 `json:"channel_vecs,omitempty"`
	// Aliases maps each known alias identifier to its canonical form.
	Aliases map[model.ID]model.ID `json:"aliases,omitempty"`
}

// Save writes the index to path as JSON, creating parent directories as needed.
func (ix *Index) Save(path string) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("index: mkdir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("index: create: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("index: close: %w", cerr)
		}
	}()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	snap := snapshot{
		Graph:           ix.Graph,
		Postings:        ix.postings,
		Texts:           ix.texts,
		ChannelPostings: ix.channelPostings,
		ChannelTexts:    ix.channelTexts,
		PersonVecs:      ix.personVecs,
		ChannelVecs:     ix.channelVecs,
		Aliases:         ix.identityResolver().Pairs(),
	}
	if err := enc.Encode(snap); err != nil {
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
	defer func() { _ = f.Close() }()

	var snap snapshot
	if err := json.NewDecoder(f).Decode(&snap); err != nil {
		return nil, fmt.Errorf("index: decode: %w", err)
	}
	ix := &Index{
		Graph:           snap.Graph,
		postings:        snap.Postings,
		texts:           snap.Texts,
		channelPostings: snap.ChannelPostings,
		channelTexts:    snap.ChannelTexts,
		personVecs:      snap.PersonVecs,
		channelVecs:     snap.ChannelVecs,
		resolver:        identity.NewResolver(),
		halfLife:        DefaultHalfLife,
		now:             time.Now,
	}
	ix.resolver.Restore(snap.Aliases)
	if ix.Graph == nil {
		ix.Graph = model.NewGraph()
	}
	if ix.Graph.Channels == nil {
		ix.Graph.Channels = make(map[model.ID]*model.Channel)
	}
	if ix.postings == nil {
		ix.postings = make(map[string]map[model.ID]float64)
	}
	if ix.texts == nil {
		ix.texts = make(map[model.ID]*personText)
	}
	if ix.channelPostings == nil {
		ix.channelPostings = make(map[string]map[model.ID]float64)
	}
	if ix.channelTexts == nil {
		ix.channelTexts = make(map[model.ID]*channelText)
	}
	if ix.personVecs == nil {
		ix.personVecs = make(map[model.ID][]float32)
	}
	if ix.channelVecs == nil {
		ix.channelVecs = make(map[model.ID][]float32)
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

// betterName reports whether name should replace current. A handle-like
// placeholder ("@login", "jira:accountid") never replaces a real name.
func betterName(current, name string) bool {
	if name == "" {
		return false
	}
	if current == "" {
		return true
	}
	return !strings.HasPrefix(name, "@") && !strings.Contains(name, ":")
}

// fillIdentity copies non-empty identity fields from rec onto p.
func fillIdentity(p *model.Person, rec connector.Record) {
	if betterName(p.Name, rec.Name) {
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
