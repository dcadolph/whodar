package bot

import (
	"strings"

	"github.com/dcadolph/whodar/internal/resolve"
)

// Format renders an answer as Slack mrkdwn.
func Format(query string, ans resolve.Answer) string {
	if len(ans.People) == 0 && len(ans.Channels) == 0 {
		return "No matches for *" + query + "*. Try different words."
	}

	var b strings.Builder
	if ans.Summary != "" {
		b.WriteString(ans.Summary)
		b.WriteString("\n\n")
	}

	if len(ans.People) > 0 {
		b.WriteString("*People*\n")
		for _, m := range ans.People {
			name := m.Person.Name
			if name == "" {
				name = m.Person.Email
			}
			line := "• " + name
			if m.Person.Title != "" {
				line += " - " + m.Person.Title
			}
			if m.Team != nil && m.Team.Name != "" {
				line += " (" + m.Team.Name + ")"
			}
			if label := resolve.ConfidenceLabel(m.Confidence); label != "" {
				line += " · " + label + " match"
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	if len(ans.Channels) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("*Channels*\n")
		for _, c := range ans.Channels {
			line := "• #" + c.Channel.Name
			if c.Channel.Topic != "" {
				line += " - " + c.Channel.Topic
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}
