package jellyfin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const movieID = "00112233445566778899aabbccddeeff"

func TestLibraryProtocolWithoutLegacyAuthorization(t *testing.T) {
	var scans int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Match a server with EnableLegacyAuthorization=false: the deprecated
		// header alone must not authenticate either discovery or refresh.
		if r.Header.Get("Authorization") != `MediaBrowser Token="test-api-key"` {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-Emby-Token") != "" || strings.Contains(r.URL.RawQuery, "test-api-key") {
			t.Error("credential duplicated into a legacy header or query")
		}
		switch r.URL.Path {
		case "/jellyfin/Library/VirtualFolders":
			if r.Method != http.MethodGet {
				t.Errorf("method = %s", r.Method)
			}
			fmt.Fprintf(w, `[{"ItemId":%q,"Name":"Movies","CollectionType":"movies","Locations":["/media/movies"]},{"ItemId":"11112233445566778899aabbccddeeff","Name":"TV","CollectionType":"tvshows","Locations":null},{"ItemId":"bad","CollectionType":"movies"},{"Name":"Music","CollectionType":"music"}]`, movieID)
		case "/jellyfin/Items/00112233-4455-6677-8899-aabbccddeeff/Refresh":
			if r.Method != http.MethodPost || r.URL.Query().Get("recursive") != "true" || r.URL.Query().Get("replaceAllMetadata") != "false" || r.URL.Query().Get("metadataRefreshMode") != "Default" {
				t.Errorf("wrong scan request: %s %s", r.Method, r.URL)
			}
			scans++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL+"/jellyfin/", "test-api-key", srv.Client())
	libraries, err := c.ListLibraries()
	if err != nil || len(libraries) != 2 {
		t.Fatalf("libraries=%+v error=%v", libraries, err)
	}
	if libraries[0].ID != movieID || libraries[0].Type != "movie" || libraries[0].Locations[0] != "/media/movies" || libraries[1].Type != "show" || libraries[1].Locations == nil {
		t.Fatalf("bad normalization: %+v", libraries)
	}
	if err := c.TestConnection(); err != nil {
		t.Fatal(err)
	}
	if err := NewClient(srv.URL+"/jellyfin", "wrong-key", srv.Client()).TestConnection(); err == nil {
		t.Fatal("invalid API key passed connection test")
	}
	if err := c.RefreshLibrary(movieID); err != nil {
		t.Fatal(err)
	}
	if err := c.RefreshLibrary("../Library/Refresh?api_key=secret"); err == nil {
		t.Fatal("invalid ID accepted")
	}
	if scans != 1 {
		t.Fatalf("scans=%d", scans)
	}
}

func TestConnectionErrorsAreSafe(t *testing.T) {
	for _, status := range []int{401, 403, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				fmt.Fprint(w, "private-server-payload test-api-key")
			}))
			defer srv.Close()
			err := NewClient(srv.URL, "test-api-key", srv.Client()).TestConnection()
			if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "test-api-key") {
				t.Fatalf("unsafe or missing error: %v", err)
			}
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "<html>login</html>") }))
	defer srv.Close()
	if err := NewClient(srv.URL, "key", srv.Client()).TestConnection(); err == nil {
		t.Fatal("non-JSON response passed connection test")
	}
}

type testSettings map[string]string

func (s testSettings) Get(key string) (string, error) { return s[key], nil }

func TestProviderReloadsCredentials(t *testing.T) {
	s := testSettings{"url": "http://first", "key": "first-key"}
	p := NewProvider(s, "url", "key", http.DefaultClient)
	first, err := p.Client()
	if err != nil {
		t.Fatal(err)
	}
	if cached, _ := p.Client(); cached != first {
		t.Fatal("client not cached")
	}
	s["url"], s["key"] = "http://second", "second-key"
	p.Invalidate()
	second, err := p.Client()
	if err != nil || second == first || second.baseURL != "http://second" || second.apiKey != "second-key" {
		t.Fatal("stale client after invalidation")
	}
	s["key"] = ""
	p.Invalidate()
	if _, err := p.Client(); err == nil {
		t.Fatal("disconnected provider still usable")
	}
}
