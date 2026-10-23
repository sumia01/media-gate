package settings

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMediaServerSettings(t *testing.T) {
	st := newSettingsStubStore()
	svc := NewService(st, t.TempDir(), nil, "encryption-test-secret", http.DefaultClient)
	if !svc.MediaServerRefreshEnabled(KeyPlexRefreshEnabled) || svc.MediaServerRefreshEnabled(KeyJellyfinRefreshEnabled) {
		t.Fatal("wrong upgrade defaults")
	}
	if err := svc.Update([]KeyValue{
		{Key: KeyPlexRefreshEnabled, Value: "false"}, {Key: KeyJellyfinRefreshEnabled, Value: "true"},
		{Key: KeyJellyfinAPIKey, Value: "private-jellyfin-key"}, {Key: "jellyfin:mapping:1", Value: "library-id"},
	}); err != nil {
		t.Fatal(err)
	}
	if svc.MediaServerRefreshEnabled(KeyPlexRefreshEnabled) || !svc.MediaServerRefreshEnabled(KeyJellyfinRefreshEnabled) {
		t.Fatal("toggle not persisted independently")
	}
	stored := st.settings[KeyJellyfinAPIKey]
	if !stored.Sensitive || stored.Value == "private-jellyfin-key" {
		t.Fatal("API key not encrypted")
	}
	items, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if strings.HasPrefix(item.Key, "jellyfin:mapping:") {
			t.Fatal("internal mapping exposed as setting")
		}
		if item.Key == KeyJellyfinAPIKey && item.Value != "****-key" {
			t.Fatalf("secret not masked: %q", item.Value)
		}
	}
	if err := svc.Update([]KeyValue{{Key: KeyJellyfinAPIKey, Value: "****-key"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Get(KeyJellyfinAPIKey); got != "private-jellyfin-key" {
		t.Fatal("mask overwrote API key")
	}
	if err := svc.Update([]KeyValue{{Key: KeyJellyfinURL, Value: "file:///tmp/library"}}); err == nil {
		t.Fatal("invalid URL accepted")
	}
}

func TestJellyfinConnectionUsesSavedOrDraftCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != `MediaBrowser Token="saved-key"` {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	svc := NewService(newSettingsStubStore(), t.TempDir(), nil, "secret", srv.Client())
	if ok, _, _ := svc.TestJellyfin(nil, nil); ok {
		t.Fatal("unconfigured connection passed")
	}
	if err := svc.Update([]KeyValue{{Key: KeyJellyfinURL, Value: srv.URL}, {Key: KeyJellyfinAPIKey, Value: "saved-key"}}); err != nil {
		t.Fatal(err)
	}
	if ok, msg, err := svc.TestJellyfin(nil, nil); err != nil || !ok {
		t.Fatalf("saved credentials failed: %s %v", msg, err)
	}
	bad := "draft-key"
	if ok, _, _ := svc.TestJellyfin(nil, &bad); ok {
		t.Fatal("draft credentials ignored")
	}
}
