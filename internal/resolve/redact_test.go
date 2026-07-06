package resolve

import (
	"context"
	"strings"
	"testing"

	"github.com/dcadolph/whodar/internal/connector"
	"github.com/dcadolph/whodar/internal/index"
)

// captureChatter records what would leave the machine and replies with a
// fixed ranking by candidate number.
type captureChatter struct {
	// system is the captured system prompt.
	system string
	// user is the captured user prompt.
	user string
	// reply is returned to the resolver.
	reply string
}

// Chat records the prompts and returns the canned reply.
func (c *captureChatter) Chat(_ context.Context, system, user string) (string, error) {
	c.system, c.user = system, user
	return c.reply, nil
}

// redactIndex builds two people whose order the model will flip.
func redactIndex() *index.Index {
	ix := index.New()
	ix.Build([]connector.Record{{
		Kind: connector.KindPerson, Email: "alice@corp.com", Name: "Alice Smith",
		Title: "Staff Engineer", Team: "Payments", Topics: []string{"billing"},
		Text: "wrote the billing retry handler", Source: "org-csv",
	}, {
		Kind: connector.KindPerson, Email: "bob@corp.com", Name: "Bob Jones",
		Title: "Engineer", Team: "Payments", Topics: []string{"billing"}, Source: "org-csv",
	}, {
		Kind: connector.KindChannel, Name: "payments", Title: "billing questions",
		Members: []string{"alice@corp.com"}, Source: "slack",
	}})
	return ix
}

func TestRedactedLLMSendsNoIdentifiers(t *testing.T) {
	t.Parallel()
	chat := &captureChatter{reply: `{"people":["2","1"],"channels":["1"]}`}
	res := NewRedactedLLM(redactIndex(), chat, nil)

	ans, err := res.Resolve(context.Background(), "billing", 5)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	sent := strings.ToLower(chat.system + "\n" + chat.user)
	for _, leak := range []string{"alice", "bob", "@corp.com", "smith", "jones"} {
		if strings.Contains(sent, leak) {
			t.Errorf("prompt leaked %q:\n%s", leak, chat.user)
		}
	}
	if !strings.Contains(chat.user, "Staff Engineer") {
		t.Errorf("prompt missing role context:\n%s", chat.user)
	}

	if len(ans.People) != 2 || ans.People[0].Person.Email != "bob@corp.com" {
		t.Errorf("people order top = %v, want the model's number ranking applied", ans.People[0].Person.ID)
	}
	if len(ans.Channels) != 1 || ans.Channels[0].Channel.Name != "payments" {
		t.Errorf("channels = %+v", ans.Channels)
	}
	if !strings.Contains(ans.Summary, "Bob Jones") {
		t.Errorf("summary = %q, want a locally written name", ans.Summary)
	}
}
