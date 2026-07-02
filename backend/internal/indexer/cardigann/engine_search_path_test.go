package cardigann

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// singularPathYAML mirrors the real-world shape of .cache/definitions/Bittorrentfiles.yml
// and houseofdevil.yml, which use the singular "search: path: <path>" key instead of
// the standard "search: paths: [...]" list. Before this fix, such definitions
// unmarshalled with an empty Search.Paths, and Engine.Search panicked on Paths[0].
const singularPathYAML = `
id: bittorrentfiles-like
name: Test Singular Path
links:
  - https://example.com/
search:
  path: browse.php
  inputs:
    search: "{{ .Keywords }}"
  rows:
    selector: tr
  fields:
    title:
      selector: a
`

const listPathYAML = `
id: list-path
name: Test List Path
links:
  - https://example.com/
search:
  paths:
    - path: torrents.php
  rows:
    selector: tr
  fields:
    title:
      selector: a
`

const noPathYAML = `
id: no-path
name: Test No Path
links:
  - https://example.com/
search:
  rows:
    selector: tr
  fields:
    title:
      selector: a
`

func TestSearch_UnmarshalYAML_SingularPath(t *testing.T) {
	def, err := ParseDefinition([]byte(singularPathYAML))
	if err != nil {
		t.Fatalf("ParseDefinition failed: %v", err)
	}

	if len(def.Search.Paths) != 1 {
		t.Fatalf("expected 1 search path, got %d", len(def.Search.Paths))
	}
	if got := def.Search.Paths[0].Path; got != "browse.php" {
		t.Errorf("got Paths[0].Path=%q, want %q", got, "browse.php")
	}
}

func TestSearch_UnmarshalYAML_PathsListStillWorks(t *testing.T) {
	def, err := ParseDefinition([]byte(listPathYAML))
	if err != nil {
		t.Fatalf("ParseDefinition failed: %v", err)
	}

	if len(def.Search.Paths) != 1 {
		t.Fatalf("expected 1 search path, got %d", len(def.Search.Paths))
	}
	if got := def.Search.Paths[0].Path; got != "torrents.php" {
		t.Errorf("got Paths[0].Path=%q, want %q", got, "torrents.php")
	}
}

func TestNewEngine_RejectsDefinitionWithNoSearchPath(t *testing.T) {
	def, err := ParseDefinition([]byte(noPathYAML))
	if err != nil {
		t.Fatalf("ParseDefinition failed: %v", err)
	}
	if len(def.Search.Paths) != 0 {
		t.Fatalf("expected 0 search paths, got %d", len(def.Search.Paths))
	}

	_, err = NewEngine(def, map[string]string{})
	if err == nil {
		t.Fatal("expected NewEngine to reject a definition with no search path, got nil error")
	}
	if !strings.Contains(err.Error(), "no path") {
		t.Errorf("expected error to mention missing path, got: %v", err)
	}
}

func TestEngine_Search_NoPath_ReturnsErrorNotPanic(t *testing.T) {
	def, err := ParseDefinition([]byte(noPathYAML))
	if err != nil {
		t.Fatalf("ParseDefinition failed: %v", err)
	}

	// Construct the engine directly, bypassing NewEngine's validation, to prove
	// Engine.Search() itself guards against an empty Paths slice rather than
	// relying solely on construction-time validation.
	e := &Engine{
		def:        def,
		config:     map[string]string{},
		httpClient: &http.Client{},
		baseURL:    "https://example.com",
		loggedIn:   true,
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Search panicked instead of returning an error: %v", r)
		}
	}()

	_, err = e.Search(context.Background(), SearchQuery{Q: "test"})
	if err == nil {
		t.Fatal("expected Search to return an error for a definition with no search path")
	}
	if !strings.Contains(err.Error(), "no path") {
		t.Errorf("expected error to mention missing path, got: %v", err)
	}
}

func TestEngine_Search_SingularPath_UsesRenderedPath(t *testing.T) {
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><table><tr><td><a href="/details.php?id=1">Some Release</a></td></tr></table></body></html>`)
	}))
	defer srv.Close()

	yamlDef := fmt.Sprintf(`
id: bittorrentfiles-like
name: Test Singular Path
links:
  - %s
search:
  path: browse.php
  inputs:
    search: "{{ .Keywords }}"
  rows:
    selector: tr
  fields:
    title:
      selector: a
`, srv.URL)

	def, err := ParseDefinition([]byte(yamlDef))
	if err != nil {
		t.Fatalf("ParseDefinition failed: %v", err)
	}

	e, err := NewEngine(def, map[string]string{})
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	e.loggedIn = true // skip login flow, not under test here

	results, err := e.Search(context.Background(), SearchQuery{Q: "test"})
	if err != nil {
		t.Fatalf("Search returned an unexpected error: %v", err)
	}

	if gotPath != "/browse.php" {
		t.Errorf("expected request path %q, got %q", "/browse.php", gotPath)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Some Release" {
		t.Errorf("got title=%q, want %q", results[0].Title, "Some Release")
	}
}
