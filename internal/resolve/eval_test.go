package resolve

import (
	"context"
	"fmt"
	"testing"

	"github.com/dcadolph/whodar/internal/connector"
	"github.com/dcadolph/whodar/internal/index"
)

// evalOrg returns a synthetic organization used to measure ranking quality. Each
// person carries strong signal (explicit topics) plus deliberate noise (a text
// mention of another person's area) so ranking must prefer the topic owner over
// an incidental mention rather than matching on any shared word.
func evalOrg() []connector.Record {
	person := func(email, name, title, team string, topics []string, text string) connector.Record {
		return connector.Record{
			Kind: connector.KindPerson, Email: email, Name: name, Title: title,
			Team: team, Topics: topics, Text: text, Source: "eval",
		}
	}
	channel := func(name, topic string, members ...string) connector.Record {
		return connector.Record{
			Kind: connector.KindChannel, Name: name, Title: topic,
			Members: members, Source: "eval",
		}
	}
	return []connector.Record{
		person("jane@corp.com", "Jane Roe", "Staff Engineer", "Payments",
			[]string{"billing", "payments", "retries", "idempotency"},
			"shipped a kubernetes job once for a migration"),
		person("bob@corp.com", "Bob Smith", "Senior Engineer", "Data Platform",
			[]string{"kafka", "streaming", "events", "pipelines"}, ""),
		person("carol@corp.com", "Carol Lee", "Site Reliability Engineer", "Infrastructure",
			[]string{"kubernetes", "deploys", "terraform", "infra"},
			"helped debug a payments outage last quarter"),
		person("dan@corp.com", "Dan Park", "Security Engineer", "Security",
			[]string{"oauth", "sso", "login", "authentication"}, ""),
		person("eve@corp.com", "Eve Ng", "Frontend Engineer", "Web",
			[]string{"react", "frontend", "typescript", "ui"}, ""),
		person("frank@corp.com", "Frank Ito", "Machine Learning Engineer", "ML Platform",
			[]string{"embeddings", "models", "inference", "ranking"}, ""),
		person("grace@corp.com", "Grace Kim", "Site Reliability Engineer", "Infrastructure",
			[]string{"oncall", "incidents", "pagerduty", "escalation"}, ""),
		person("heidi@corp.com", "Heidi Cho", "Search Engineer", "Search",
			[]string{"elasticsearch", "indexing", "relevance", "search"}, ""),
		channel("payments", "billing and payments questions", "jane@corp.com"),
		channel("data-platform", "kafka and streaming", "bob@corp.com"),
		channel("infra", "kubernetes deploys and oncall", "carol@corp.com", "grace@corp.com"),
		channel("security", "auth login and sso", "dan@corp.com"),
	}
}

// buildEvalIndex builds a keyword index over the synthetic organization.
func buildEvalIndex() *index.Index {
	ix := index.New()
	ix.Build(evalOrg())
	return ix
}

// personRank returns the one-based rank of email among the resolved people, or
// zero when it is absent.
func personRank(ans Answer, email string) int {
	for i, m := range ans.People {
		if m.Person.Email == email {
			return i + 1
		}
	}
	return 0
}

// channelRank returns the one-based rank of a channel name among the resolved
// channels, or zero when it is absent.
func channelRank(ans Answer, name string) int {
	for i, c := range ans.Channels {
		if c.Channel.Name == name {
			return i + 1
		}
	}
	return 0
}

// TestKeywordRankingQuality scores the keyword resolver against a golden set and
// asserts a quality floor, turning ranking accuracy into a number that fails
// loudly on regression. It reports hit-at-one and mean reciprocal rank for both
// people and channels.
func TestKeywordRankingQuality(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Query       string
		WantPerson  string
		WantChannel string
	}{{ // Test 0: Explicit topic beats an incidental text mention by another person.
		Query: "who do I talk to about billing retries", WantPerson: "jane@corp.com", WantChannel: "payments",
	}, { // Test 1: Streaming pipeline owner.
		Query: "kafka streaming pipelines", WantPerson: "bob@corp.com", WantChannel: "data-platform",
	}, { // Test 2: Infra owner wins over a payments distractor in Carol's text.
		Query: "kubernetes deploys", WantPerson: "carol@corp.com", WantChannel: "infra",
	}, { // Test 3: Auth owner.
		Query: "oauth sso login", WantPerson: "dan@corp.com", WantChannel: "security",
	}, { // Test 4: Frontend owner, no expected channel.
		Query: "react frontend ui", WantPerson: "eve@corp.com",
	}, { // Test 5: Machine learning owner.
		Query: "embeddings model inference", WantPerson: "frank@corp.com",
	}, { // Test 6: On-call owner shares the infra channel with Carol.
		Query: "oncall incident escalation", WantPerson: "grace@corp.com", WantChannel: "infra",
	}, { // Test 7: Search owner.
		Query: "elasticsearch search relevance", WantPerson: "heidi@corp.com",
	}}

	ix := buildEvalIndex()
	resolver := NewKeyword(ix)

	var personRR, personHits, channelRR, channelHits float64
	var channelCases float64
	for testNum, test := range tests {
		t.Run(fmt.Sprintf("test %d", testNum), func(t *testing.T) {
			t.Parallel()
			ans, err := resolver.Resolve(context.Background(), test.Query, 8)
			if err != nil {
				t.Fatalf("resolve %q: %v", test.Query, err)
			}
			if rank := personRank(ans, test.WantPerson); rank != 1 {
				t.Errorf("query %q: want %s ranked 1, got rank %d", test.Query, test.WantPerson, rank)
			}
			if test.WantChannel != "" {
				if rank := channelRank(ans, test.WantChannel); rank != 1 {
					t.Errorf("query %q: want channel #%s ranked 1, got rank %d",
						test.Query, test.WantChannel, rank)
				}
			}
		})

		ans, err := resolver.Resolve(context.Background(), test.Query, 8)
		if err != nil {
			t.Fatalf("resolve %q: %v", test.Query, err)
		}
		if rank := personRank(ans, test.WantPerson); rank > 0 {
			personRR += 1 / float64(rank)
			if rank == 1 {
				personHits++
			}
		}
		if test.WantChannel != "" {
			channelCases++
			if rank := channelRank(ans, test.WantChannel); rank > 0 {
				channelRR += 1 / float64(rank)
				if rank == 1 {
					channelHits++
				}
			}
		}
	}

	n := float64(len(tests))
	personMRR, personHitAt1 := personRR/n, personHits/n
	channelMRR, channelHitAt1 := channelRR/channelCases, channelHits/channelCases
	t.Logf("people:   hit@1 %.2f  MRR %.2f  (%d queries)", personHitAt1, personMRR, len(tests))
	t.Logf("channels: hit@1 %.2f  MRR %.2f  (%.0f queries)", channelHitAt1, channelMRR, channelCases)

	// Quality floor. The synthetic set is designed so the topic owner wins every
	// query, so healthy ranking scores 1.0. The floor sits below that to catch a
	// real regression without flaking on a single reordering.
	const floorHitAt1, floorMRR = 0.75, 0.85
	if personHitAt1 < floorHitAt1 {
		t.Errorf("people hit@1 %.2f below floor %.2f", personHitAt1, floorHitAt1)
	}
	if personMRR < floorMRR {
		t.Errorf("people MRR %.2f below floor %.2f", personMRR, floorMRR)
	}
	if channelHitAt1 < floorHitAt1 {
		t.Errorf("channel hit@1 %.2f below floor %.2f", channelHitAt1, floorHitAt1)
	}
	if channelMRR < floorMRR {
		t.Errorf("channel MRR %.2f below floor %.2f", channelMRR, floorMRR)
	}
}
