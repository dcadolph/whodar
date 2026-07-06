package index

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dcadolph/whodar/internal/connector"
)

// sampleRecords returns a small set of people for ranking tests.
func sampleRecords() []connector.Record {
	return []connector.Record{
		{Source: "t", Name: "Jane Roe", Email: "jane@x.com", Title: "Staff Engineer",
			Team: "Billing", Topics: []string{"retries", "idempotency"}},
		{Source: "t", Name: "Bob Lee", Email: "bob@x.com", Title: "SRE",
			Team: "Infra", Topics: []string{"kafka"}},
		{Source: "t", Name: "Carol Ng", Email: "carol@x.com", Title: "Engineer",
			Team: "Billing", Text: "owns the billing dashboards"},
	}
}

// TestSearchRanking verifies the most relevant person ranks first with reasons.
func TestSearchRanking(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name       string
		Query      string
		WantTop    string
		WantReason string
	}{
		{Name: "topic", Query: "retries", WantTop: "jane@x.com", WantReason: "retries (topic)"},
		{Name: "team text", Query: "billing dashboards", WantTop: "carol@x.com",
			WantReason: "dashboards (mention)"},
		{Name: "title", Query: "sre", WantTop: "bob@x.com", WantReason: "sre (title)"},
	}

	ix := New()
	ix.Build(sampleRecords())

	for testNum, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			t.Parallel()
			got := ix.Search(test.Query, 5)
			if len(got) == 0 {
				t.Fatalf("test %d: no matches for %q", testNum, test.Query)
			}
			if string(got[0].Person.ID) != test.WantTop {
				t.Errorf("test %d: top = %q, want %q", testNum, got[0].Person.ID, test.WantTop)
			}
			if !slices.Contains(got[0].Reasons, test.WantReason) {
				t.Errorf("test %d: reasons %v missing %q", testNum, got[0].Reasons, test.WantReason)
			}
		})
	}
}

// TestSearchEmpty verifies a stopword-only query yields nothing.
func TestSearchEmpty(t *testing.T) {
	t.Parallel()
	ix := New()
	ix.Build(sampleRecords())
	if got := ix.Search("who do I talk to about", 5); len(got) != 0 {
		t.Errorf("got %d matches, want 0", len(got))
	}
}

// TestSaveLoad verifies an index survives a round trip to disk.
func TestSaveLoad(t *testing.T) {
	t.Parallel()
	ix := New()
	ix.Build(sampleRecords())

	path := filepath.Join(t.TempDir(), "index.json")
	if err := ix.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	got := loaded.Search("retries", 1)
	if len(got) != 1 || string(got[0].Person.ID) != "jane@x.com" {
		t.Fatalf("after load, top match = %v, want jane@x.com", got)
	}
	if got[0].Team == nil || got[0].Team.Name != "Billing" {
		t.Errorf("team not restored: %+v", got[0].Team)
	}
}

// TestTokenize covers lowercasing, splitting, and stopword removal.
func TestTokenize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		In   string
		Want []string
	}{
		{In: "Who do I talk to about Billing-Retries?", Want: []string{"billing", "retries"}},
		{In: "Kafka, Kubernetes & SRE", Want: []string{"kafka", "kubernetes", "sre"}},
		{In: "a I to the", Want: nil},
	}
	for testNum, test := range tests {
		t.Run(test.In, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(test.Want, tokenize(test.In), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("test %d: mismatch (-want +got):\n%s", testNum, diff)
			}
		})
	}
}

// TestSlug covers identifier normalization.
func TestSlug(t *testing.T) {
	t.Parallel()
	tests := []struct {
		In   string
		Want string
	}{
		{In: "Site Reliability Eng!", Want: "site-reliability-eng"},
		{In: "  Billing / Payments  ", Want: "billing-payments"},
		{In: "A_B-C", Want: "a-b-c"},
	}
	for testNum, test := range tests {
		t.Run(test.In, func(t *testing.T) {
			t.Parallel()
			if got := slug(test.In); got != test.Want {
				t.Errorf("test %d: slug(%q) = %q, want %q", testNum, test.In, got, test.Want)
			}
		})
	}
}

// TestSearchChannels verifies channel ranking, reasons, and member ordering.
func TestSearchChannels(t *testing.T) {
	t.Parallel()
	ix := New()
	ix.Build([]connector.Record{
		{Name: "Jane Roe", Email: "jane@x.com", Title: "Engineer", Topics: []string{"retries"}},
		{Name: "Bob Lee", Email: "bob@x.com", Title: "SRE", Topics: []string{"kafka"}},
		{Kind: connector.KindChannel, Name: "billing", Title: "retries and dunning",
			Members: []string{"jane@x.com", "bob@x.com"}, Text: "billing platform"},
	})

	got := ix.SearchChannels("retries", 5)
	if len(got) == 0 {
		t.Fatal("no channel matches for retries")
	}
	if got[0].Channel.Name != "billing" {
		t.Errorf("top channel = %q, want billing", got[0].Channel.Name)
	}
	if !slices.Contains(got[0].Reasons, "retries (topic)") {
		t.Errorf("reasons %v missing retries (topic)", got[0].Reasons)
	}
	if len(got[0].TopMembers) == 0 || got[0].TopMembers[0].Email != "jane@x.com" {
		t.Errorf("top member = %v, want jane@x.com first", got[0].TopMembers)
	}
}

// TestStemMatching verifies a query matches indexed terms across word forms.
func TestStemMatching(t *testing.T) {
	t.Parallel()
	ix := New()
	ix.Build([]connector.Record{
		{Name: "Jane Roe", Email: "jane@x.com", Topics: []string{"scanning"}},
	})
	for _, q := range []string{"scans", "scan", "scanning"} {
		got := ix.Search(q, 1)
		if len(got) != 1 || got[0].Person.Email != "jane@x.com" {
			t.Errorf("query %q: got %v, want jane@x.com", q, got)
		}
	}
}

// TestAddMerges verifies Add accumulates onto an existing index: it keeps old
// signal, extends a person shared by email, and adds new people.
func TestAddMerges(t *testing.T) {
	t.Parallel()
	ix := New()
	ix.Build([]connector.Record{
		{Name: "Jane", Email: "jane@x.com", Topics: []string{"billing"}},
	})
	ix.Add([]connector.Record{
		{Name: "Jane Roe", Email: "jane@x.com", Topics: []string{"retries"}},
		{Name: "Bob", Email: "bob@x.com", Topics: []string{"kafka"}},
	})

	if got := ix.Search("billing", 1); len(got) != 1 || got[0].Person.Email != "jane@x.com" {
		t.Errorf("billing kept after merge: %v", got)
	}
	if got := ix.Search("retries", 1); len(got) != 1 || got[0].Person.Email != "jane@x.com" {
		t.Errorf("jane gained retries from merge: %v", got)
	}
	if got := ix.Search("kafka", 1); len(got) != 1 || got[0].Person.Email != "bob@x.com" {
		t.Errorf("bob added by merge: %v", got)
	}
}

// TestProfile verifies the full-person lookup resolves aliases and gathers
// org placement and channel membership.
func TestProfile(t *testing.T) {
	t.Parallel()
	ix := New()
	ix.Alias("alice@corp.com", "github:alice")
	ix.Build([]connector.Record{{
		Kind: connector.KindPerson, Email: "alice@corp.com", Name: "Alice Smith",
		Title: "Engineer", Team: "Payments", Org: "Corp", Manager: "boss@corp.com",
		Topics: []string{"billing"}, Source: "org-csv",
	}, {
		Kind: connector.KindPerson, Email: "boss@corp.com", Name: "Boss", Source: "org-csv",
	}, {
		Kind: connector.KindChannel, Name: "payments", Title: "billing questions",
		Members: []string{"alice@corp.com"}, Source: "slack",
	}})

	got, ok := ix.Profile("github:alice")
	if !ok {
		t.Fatal("Profile(github:alice) not found; alias should resolve")
	}
	if got.Person.Name != "Alice Smith" || got.Team == nil || got.Team.Name != "Payments" {
		t.Errorf("profile person/team = %+v / %+v", got.Person, got.Team)
	}
	if got.Org == nil || got.Org.Name != "Corp" {
		t.Errorf("profile org = %+v", got.Org)
	}
	if got.Manager == nil || got.Manager.Name != "Boss" {
		t.Errorf("profile manager = %+v", got.Manager)
	}
	if len(got.Channels) != 1 || got.Channels[0].Name != "payments" {
		t.Errorf("profile channels = %+v", got.Channels)
	}
	if _, ok := ix.Profile("nobody@corp.com"); ok {
		t.Error("Profile(nobody) = ok, want false")
	}
}
