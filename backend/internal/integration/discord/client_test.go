package discord

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendDisablesAllMentions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request: %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		var payload webhookPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.AllowedMentions.Parse == nil || len(payload.AllowedMentions.Parse) != 0 {
			t.Errorf("expected allowed_mentions.parse=[], got %+v", payload.AllowedMentions)
		}
		if len(payload.Embeds) == 0 {
			t.Error("missing embed")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	client := NewClient(srv.URL, srv.Client())
	if err := client.Send(NewEmbed().Title("@everyone <@123> <@&456>").Description("@here")); err != nil {
		t.Fatal(err)
	}
	if ok, _, err := TestConnection(srv.URL, srv.Client()); err != nil || !ok {
		t.Fatalf("connection test failed: ok=%v err=%v", ok, err)
	}
}

func TestSendHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if err := NewClient(srv.URL, srv.Client()).Send(NewEmbed().Title("Failure")); err == nil {
		t.Error("expected webhook HTTP error")
	}
}
