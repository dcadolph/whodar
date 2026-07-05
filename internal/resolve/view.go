package resolve

import "math"

// Confidence labels partition the zero-to-one confidence range for display.
const (
	// strongConfidence is the floor of a strong match.
	strongConfidence = 0.75
	// moderateConfidence is the floor of a moderate match.
	moderateConfidence = 0.45
)

// ConfidenceLabel names a confidence value for display: strong, moderate, or
// weak. It returns the empty string for zero, when confidence is unknown.
func ConfidenceLabel(c float64) string {
	switch {
	case c <= 0:
		return ""
	case c >= strongConfidence:
		return "strong"
	case c >= moderateConfidence:
		return "moderate"
	default:
		return "weak"
	}
}

// roundConfidence trims a confidence to two decimals for stable JSON output.
func roundConfidence(c float64) float64 {
	return math.Round(c*100) / 100
}

// JSONAnswer is a flat, JSON-friendly view of an Answer, shared by the CLI and
// the web server so both emit the same shape.
type JSONAnswer struct {
	// Query echoes the question asked.
	Query string `json:"query,omitempty"`
	// Summary is the written recommendation, present in LLM mode.
	Summary string `json:"summary,omitempty"`
	// People is the ranked list of people to talk to.
	People []JSONPerson `json:"people"`
	// Channels is the ranked list of places to ask.
	Channels []JSONChannel `json:"channels,omitempty"`
}

// JSONPerson is one ranked person.
type JSONPerson struct {
	// Name is the person's display name.
	Name string `json:"name"`
	// Email is the person's work email.
	Email string `json:"email,omitempty"`
	// Title is the person's job title.
	Title string `json:"title,omitempty"`
	// Team is the person's team name.
	Team string `json:"team,omitempty"`
	// Identities lists alternate identifiers merged into this person, such as
	// a GitHub login joined to an email.
	Identities []string `json:"identities,omitempty"`
	// Score is the relevance score.
	Score float64 `json:"score"`
	// Confidence estimates how trustworthy the match is, from zero to one.
	// Zero means unknown and is omitted.
	Confidence float64 `json:"confidence,omitempty"`
	// Reasons explains why the person matched.
	Reasons []string `json:"reasons,omitempty"`
}

// JSONChannel is one ranked channel.
type JSONChannel struct {
	// Name is the channel name.
	Name string `json:"name"`
	// Topic is the channel's stated topic.
	Topic string `json:"topic,omitempty"`
	// Score is the relevance score.
	Score float64 `json:"score"`
	// Confidence estimates how trustworthy the match is, from zero to one.
	// Zero means unknown and is omitted.
	Confidence float64 `json:"confidence,omitempty"`
	// Reasons explains why the channel matched.
	Reasons []string `json:"reasons,omitempty"`
	// Members are the most relevant people active in the channel.
	Members []JSONMember `json:"members,omitempty"`
}

// JSONMember is one active channel member.
type JSONMember struct {
	// Name is the member's display name.
	Name string `json:"name"`
	// Email is the member's work email.
	Email string `json:"email,omitempty"`
}

// View renders the answer as a flat JSONAnswer for the given query.
func (a Answer) View(query string) JSONAnswer {
	out := JSONAnswer{
		Query:   query,
		Summary: a.Summary,
		People:  make([]JSONPerson, 0, len(a.People)),
	}
	for _, m := range a.People {
		jp := JSONPerson{
			Name:       m.Person.Name,
			Email:      m.Person.Email,
			Title:      m.Person.Title,
			Score:      m.Score,
			Confidence: roundConfidence(m.Confidence),
			Reasons:    m.Reasons,
		}
		for _, id := range m.Person.Identities {
			jp.Identities = append(jp.Identities, string(id))
		}
		if m.Team != nil {
			jp.Team = m.Team.Name
		}
		out.People = append(out.People, jp)
	}
	for _, c := range a.Channels {
		jc := JSONChannel{
			Name:       c.Channel.Name,
			Topic:      c.Channel.Topic,
			Score:      c.Score,
			Confidence: roundConfidence(c.Confidence),
			Reasons:    c.Reasons,
		}
		for _, p := range c.TopMembers {
			jc.Members = append(jc.Members, JSONMember{Name: p.Name, Email: p.Email})
		}
		out.Channels = append(out.Channels, jc)
	}
	return out
}
