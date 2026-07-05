package index

import (
	"math"
	"strings"

	"github.com/dcadolph/whodar/internal/feedback"
	"github.com/dcadolph/whodar/internal/model"
)

// Feedback application bounds. A few votes adjust ranking; they never bury
// the underlying evidence.
const (
	// maxFeedbackNet clamps the net votes applied to one result per query.
	maxFeedbackNet = 3
	// feedbackStep is the per-vote score multiplier.
	feedbackStep = 1.25
)

// fbRule is one preprocessed vote: the stemmed query terms it applies to, the
// target entity, and the vote direction.
type fbRule struct {
	// terms are the stemmed tokens of the voted query.
	terms []string
	// id is the voted person or channel.
	id model.ID
	// vote is feedback.Helpful or feedback.NotHelpful.
	vote int
	// channel marks a channel vote.
	channel bool
}

// SetFeedback gives the index user votes to apply during ranking. Later calls
// replace earlier ones. Person votes resolve through identity aliases, so a
// vote on any of a person's identifiers lands on the canonical entry.
func (ix *Index) SetFeedback(entries []feedback.Entry) {
	rules := make([]fbRule, 0, len(entries))
	for _, e := range entries {
		if !e.Valid() {
			continue
		}
		raw := tokenize(e.Query)
		if len(raw) == 0 {
			continue
		}
		terms := make([]string, len(raw))
		for i, t := range raw {
			terms[i] = stem(t)
		}
		r := fbRule{terms: terms, vote: e.Vote}
		if e.Person != "" {
			r.id = ix.identityResolver().Canonical(model.ID(strings.ToLower(e.Person)))
		} else {
			r.id = model.ID(slug(e.Channel))
			r.channel = true
		}
		rules = append(rules, r)
	}
	ix.fbRules.Store(&rules)
}

// feedbackNets sums the votes that apply to the current query, per entity,
// clamped to maxFeedbackNet in either direction. A vote applies when every
// term of its recorded query appears in the current one.
func (ix *Index) feedbackNets(queryTerms []string, channel bool) map[model.ID]int {
	loaded := ix.fbRules.Load()
	if loaded == nil || len(*loaded) == 0 {
		return nil
	}
	qset := make(map[string]bool, len(queryTerms))
	for _, t := range queryTerms {
		qset[stem(t)] = true
	}
	nets := make(map[model.ID]int)
	for _, r := range *loaded {
		if r.channel != channel {
			continue
		}
		applies := true
		for _, t := range r.terms {
			if !qset[t] {
				applies = false
				break
			}
		}
		if applies {
			nets[r.id] += r.vote
		}
	}
	for id, n := range nets {
		nets[id] = min(max(n, -maxFeedbackNet), maxFeedbackNet)
	}
	return nets
}

// feedbackFactor converts a net vote count into a score multiplier.
func feedbackFactor(net int) float64 {
	return math.Pow(feedbackStep, float64(net))
}

// feedbackReason describes an applied net vote for the reasons list, or
// returns the empty string when no votes applied.
func feedbackReason(net int) string {
	switch {
	case net > 0:
		return "boosted by feedback"
	case net < 0:
		return "lowered by feedback"
	default:
		return ""
	}
}
