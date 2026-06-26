package pagerduty

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestServicesAndOnCalls verifies both endpoints decode.
func TestServicesAndOnCalls(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/services", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"more":false,"services":[{"id":"S1","name":"Billing API",`+
			`"description":"Handles billing and payments","escalation_policy":{"id":"EP1"}}]}`)
	})
	mux.HandleFunc("/oncalls", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"more":false,"oncalls":[{`+
			`"user":{"id":"U1","name":"Jane Roe","email":"jane@x.com"},`+
			`"escalation_policy":{"id":"EP1"}}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := New("token", WithBaseURL(srv.URL))

	services, err := c.Services(context.Background())
	if err != nil || len(services) != 1 || services[0].Name != "Billing API" ||
		services[0].EscalationPolicy.ID != "EP1" {
		t.Fatalf("services = %v, err %v", services, err)
	}
	oncalls, err := c.OnCalls(context.Background())
	if err != nil || len(oncalls) != 1 || oncalls[0].User.Email != "jane@x.com" ||
		oncalls[0].EscalationPolicy.ID != "EP1" {
		t.Fatalf("oncalls = %v, err %v", oncalls, err)
	}
}

// TestNewEmptyTokenPanics verifies the constructor guards an empty token.
func TestNewEmptyTokenPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("New(\"\") did not panic")
		}
	}()
	New("")
}
