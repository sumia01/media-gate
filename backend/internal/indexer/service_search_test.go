package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/sumia01/media-gate/internal/indexer/cardigann"
	"github.com/sumia01/media-gate/internal/store"
)

type searchTestStore struct {
	store.Store
	indexers []store.Indexer
	err      error
}

func (s *searchTestStore) ListIndexers() ([]store.Indexer, error) {
	return s.indexers, s.err
}

func TestSearchWithDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good":
			fmt.Fprint(w, `<table><tr><td class="title">High</td><td class="seeders">30</td></tr><tr><td class="title">Low</td><td class="seeders">1</td></tr></table>`)
		case "/other":
			fmt.Fprint(w, `<table><tr><td class="title">Medium</td><td class="seeders">20</td></tr></table>`)
		case "/empty":
			fmt.Fprint(w, `<table></table>`)
		default:
			http.Error(w, "https://user:password@tracker.example/?apikey=secret private response", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	for _, tc := range []struct {
		name       string
		paths      []string
		disabled   bool
		params     SearchParams
		cancel     bool
		want       SearchDiagnostics
		wantTitles []string
	}{
		{name: "none configured"},
		{name: "none enabled", paths: []string{"good"}, disabled: true},
		{name: "genuine zero results", paths: []string{"empty"}, want: SearchDiagnostics{Attempted: 1}},
		{name: "all HTTP failures", paths: []string{"failure", "failure"}, want: SearchDiagnostics{Attempted: 2, Failed: 2}},
		{name: "engine initialization failure", paths: []string{"unknown"}, want: SearchDiagnostics{Attempted: 1, Failed: 1}},
		{name: "panic counted and engine unlocked", paths: []string{"panic"}, want: SearchDiagnostics{Attempted: 1, Failed: 1}},
		{name: "partial zero results", paths: []string{"empty", "failure"}, want: SearchDiagnostics{Attempted: 2, Failed: 1}},
		{name: "mixed ranked results", paths: []string{"good", "failure", "other", "unknown"}, want: SearchDiagnostics{Attempted: 4, Failed: 2}, wantTitles: []string{"High", "Medium", "Low"}},
		{name: "rank before limit", paths: []string{"good", "failure", "other"}, params: SearchParams{Limit: 2}, want: SearchDiagnostics{Attempted: 3, Failed: 1}, wantTitles: []string{"High", "Medium"}},
		{name: "explicit disabled indexer remains searchable", paths: []string{"good"}, disabled: true, params: SearchParams{IndexerIDs: []uint{1}}, want: SearchDiagnostics{Attempted: 1}, wantTitles: []string{"High", "Low"}},
		{name: "unknown explicit id", paths: []string{"good"}, params: SearchParams{IndexerIDs: []uint{99}}},
		{name: "canceled searches", paths: []string{"good", "other"}, cancel: true, want: SearchDiagnostics{Attempted: 2, Failed: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &searchTestStore{}
			svc := &Service{store: st, engines: make(map[uint]*engineEntry)}
			for i, path := range tc.paths {
				id := uint(i + 1)
				st.indexers = append(st.indexers, store.Indexer{ID: id, Name: "private-name", Enabled: !tc.disabled})
				if path == "unknown" {
					continue
				}
				if path == "panic" {
					svc.engines[id] = &engineEntry{}
					continue
				}
				def, err := cardigann.ParseDefinition([]byte(fmt.Sprintf(`
id: test
name: Test
links: [%s]
search:
  path: %s
  rows:
    selector: tr
  fields:
    title:
      selector: .title
    seeders:
      selector: .seeders
`, server.URL, path)))
				if err != nil {
					t.Fatal(err)
				}
				engine, err := cardigann.NewEngine(def, nil)
				if err != nil {
					t.Fatal(err)
				}
				svc.engines[id] = &engineEntry{engine: engine}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			results, diagnostics, err := svc.SearchWithDiagnostics(ctx, tc.params)
			if err != nil || diagnostics != tc.want {
				t.Fatalf("diagnostics = %+v, error = %v; want %+v", diagnostics, err, tc.want)
			}
			var titles []string
			for _, result := range results {
				titles = append(titles, result.Title)
			}
			if !reflect.DeepEqual(titles, tc.wantTitles) {
				t.Fatalf("titles = %v, want %v", titles, tc.wantTitles)
			}
			// Existing callers still get the same ranked/truncated results and no
			// new error when some or all individual indexers fail.
			manual, err := svc.Search(ctx, tc.params)
			if err != nil || !reflect.DeepEqual(manual, results) {
				t.Fatalf("Search wrapper changed behavior: %+v, %v", manual, err)
			}
			encoded, err := json.Marshal(diagnostics)
			if err != nil {
				t.Fatal(err)
			}
			wantJSON := fmt.Sprintf(`{"Attempted":%d,"Failed":%d}`, tc.want.Attempted, tc.want.Failed)
			if string(encoded) != wantJSON {
				t.Fatalf("diagnostics contain more than safe counts: %s", encoded)
			}
		})
	}
}

func TestSearchDiagnosticsListFailure(t *testing.T) {
	want := errors.New("cannot list indexers")
	svc := &Service{store: &searchTestStore{err: want}}
	results, diagnostics, err := svc.SearchWithDiagnostics(context.Background(), SearchParams{})
	if !errors.Is(err, want) || results != nil || diagnostics != (SearchDiagnostics{}) {
		t.Fatalf("results = %v, diagnostics = %+v, error = %v", results, diagnostics, err)
	}
	if _, err := svc.Search(context.Background(), SearchParams{}); !errors.Is(err, want) {
		t.Fatalf("Search wrapper lost original error: %v", err)
	}
}
