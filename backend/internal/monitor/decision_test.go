package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/indexer"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
)

var privateError = errors.New("https://user:password@tracker.example/?apikey=secret: private response body")

type decisionSearch struct {
	results     []indexer.TorrentResult
	err         error
	calls       int
	after       func()
	diagnostics indexer.SearchDiagnostics
}

func (s *decisionSearch) SearchWithDiagnostics(context.Context, indexer.SearchParams) ([]indexer.TorrentResult, indexer.SearchDiagnostics, error) {
	s.calls++
	if s.after != nil {
		s.after()
	}
	return s.results, s.diagnostics, s.err
}

type decisionTestStore struct {
	store.Store
	fail      string
	duplicate bool
	listCalls int
}

func (s *decisionTestStore) WithTx(fn func(store.Store) error) error {
	return s.Store.WithTx(func(tx store.Store) error {
		wrapped := *s
		wrapped.Store = tx
		err := fn(&wrapped)
		s.listCalls = wrapped.listCalls
		return err
	})
}

func (s *decisionTestStore) HasActiveDownloadByURL(id uint, url string) (bool, error) {
	if s.fail == "dedup" {
		return false, privateError
	}
	if s.duplicate {
		return true, nil
	}
	return s.Store.HasActiveDownloadByURL(id, url)
}

func (s *decisionTestStore) IsBlocklisted(id uint, url string, threshold int) (bool, error) {
	if s.fail == "blocklist" {
		return false, privateError
	}
	return s.Store.IsBlocklisted(id, url, threshold)
}

func (s *decisionTestStore) CreateDownload(dl *store.Download) error {
	if s.fail == "create" {
		return privateError
	}
	return s.Store.CreateDownload(dl)
}

func (s *decisionTestStore) ListDownloads(id *uint, status *string) ([]store.Download, error) {
	s.listCalls++
	if s.fail == "downloads" || (s.fail == "fresh_downloads" && s.listCalls > 1) {
		return nil, privateError
	}
	return s.Store.ListDownloads(id, status)
}

func (s *decisionTestStore) ListEpisodeMonitorsByMediaItem(id uint) ([]store.EpisodeMonitor, error) {
	if s.fail == "overrides" {
		return nil, privateError
	}
	return s.Store.ListEpisodeMonitorsByMediaItem(id)
}

func newDecisionTest(t *testing.T, mediaType string) (*Service, *decisionTestStore, *decisionSearch, *store.MediaItem) {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.New(filepath.Join(dir, "monitor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	st := &decisionTestStore{Store: db}
	lib := &store.Library{Name: "Library", Path: dir, MediaType: mediaType}
	if err := st.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	item := &store.MediaItem{LibraryID: lib.ID, Title: "Show", MediaType: mediaType, Monitored: true, UpdatedAt: time.Now().UTC().Add(-time.Hour)}
	if err := st.CreateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateMediaMetadata(&store.MediaMetadata{
		MediaItemID: item.ID, Title: "Show", Source: "tmdb", ExternalID: 1, ReleaseDate: "2000-01-01",
	}); err != nil {
		t.Fatal(err)
	}
	if mediaType == "series" {
		for ep := 1; ep <= 2; ep++ {
			if err := st.CreateEpisode(&store.Episode{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: ep, AirDate: "2000-01-01"}); err != nil {
				t.Fatal(err)
			}
		}
		if err := st.CreateSeasonMonitor(&store.SeasonMonitor{MediaItemID: item.ID, SeasonNumber: 1, Monitored: true}); err != nil {
			t.Fatal(err)
		}
	}
	search := &decisionSearch{diagnostics: indexer.SearchDiagnostics{Attempted: 1}}
	svc := NewService(st, nil, settings.NewService(st, dir, nil, "test-key", http.DefaultClient), eventbus.New(16))
	svc.indexerSvc = search
	return svc, st, search, item
}

func latestDecision(t *testing.T, st store.Store, id uint) *store.MonitorDecision {
	t.Helper()
	d, err := st.GetMonitorDecision(id)
	if err != nil {
		t.Fatal(err)
	}
	if d.CheckedAt.IsZero() || d.Summary == "" || len(d.Details) == 0 {
		t.Fatalf("incomplete decision: %+v", d)
	}
	encoded, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"https://", "magnet:", "password", "apikey", "private response", "secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("unsafe snapshot contains %q: %s", secret, encoded)
		}
	}
	return d
}

func TestMonitorSearchDecisions(t *testing.T) {
	for _, mediaType := range []string{"movie", "series"} {
		for _, branch := range []string{"no_results", "profile_rejected", "grabbed", "duplicate", "blocked", "indexer_error", "dedup", "blocklist", "create", "fresh_downloads"} {
			t.Run(mediaType+"/"+branch, func(t *testing.T) {
				svc, st, search, item := newDecisionTest(t, mediaType)
				search.results = []indexer.TorrentResult{{Title: "Show.S01.Complete.1080p", DownloadURL: "https://tracker.example/?apikey=secret"}}
				want := branch
				switch branch {
				case "no_results":
					search.results = nil
				case "profile_rejected":
					profile := &store.MediaProfile{Name: "4K", Resolutions: `["2160p"]`, Languages: `[]`}
					if err := st.CreateMediaProfile(profile); err != nil {
						t.Fatal(err)
					}
					item.MediaProfileID = &profile.ID
				case "duplicate":
					st.duplicate = true
					want = "active_download"
				case "blocked":
					if err := st.RecordBlocklistFailure(item.ID, search.results[0].DownloadURL, "release", privateError.Error(), 3); err != nil {
						t.Fatal(err)
					}
				case "indexer_error":
					search.err = privateError
				case "dedup", "blocklist", "create", "fresh_downloads":
					st.fail = branch
					want = "error"
				}
				svc.processItem(item)
				d := latestDecision(t, st, item.ID)
				if d.Outcome != want || search.calls != 1 {
					t.Fatalf("decision = %+v, searches = %d, want %s", d, search.calls, want)
				}
				downloads, err := st.Store.ListDownloads(&item.ID, nil)
				if err != nil {
					t.Fatal(err)
				}
				if (len(downloads) == 1) != (branch == "grabbed") {
					t.Fatalf("downloads = %+v for branch %s", downloads, branch)
				}
				if mediaType == "series" && branch != "grabbed" && item.MonitorSearchStartedAt == nil {
					t.Fatal("failed or skipped selection was treated as foundAny")
				}
				for _, detail := range d.Details {
					if detail.Outcome == "profile_rejected" && (detail.TotalResults != 1 || detail.RejectedResults != 1) {
						t.Fatalf("wrong rejection counters: %+v", detail)
					}
					if detail.Outcome == "blocked" && detail.BlockedResults != 1 {
						t.Fatalf("wrong block counter: %+v", detail)
					}
					if detail.Outcome == "grabbed" && (detail.DownloadID == nil || *detail.DownloadID != downloads[0].ID) {
						t.Fatalf("grab not linked to real download: %+v", detail)
					}
				}
			})
		}
	}
}

func TestMonitorEligibilityAndOverwrite(t *testing.T) {
	for _, branch := range []string{"disabled", "missing_metadata", "unaired", "already_present", "active_download", "downloads"} {
		t.Run(branch, func(t *testing.T) {
			svc, st, search, item := newDecisionTest(t, "movie")
			svc.processOnce()
			before := latestDecision(t, st, item.ID)
			switch branch {
			case "disabled":
				item.Monitored = false
			case "missing_metadata":
				if err := st.DeleteMediaMetadataByMediaItem(item.ID); err != nil {
					t.Fatal(err)
				}
			case "unaired":
				meta, _ := st.GetMediaMetadataByMediaItem(item.ID)
				meta.ReleaseDate = "2999-01-01"
				if err := st.UpdateMediaMetadata(meta); err != nil {
					t.Fatal(err)
				}
			case "already_present":
				if err := st.CreateMediaFile(&store.MediaFile{MediaItemID: item.ID, Path: "/movie.mkv", FileName: "movie.mkv"}); err != nil {
					t.Fatal(err)
				}
			case "active_download":
				if err := st.CreateDownload(&store.Download{MediaItemID: item.ID, Title: "Movie", DownloadURL: "magnet:?secret", Status: "pending"}); err != nil {
					t.Fatal(err)
				}
			case "downloads":
				st.fail = branch
			}
			svc.processItem(item)
			d := latestDecision(t, st, item.ID)
			want := branch
			if branch == "downloads" {
				want = "error"
			}
			if d.Outcome != want || !d.CheckedAt.After(before.CheckedAt) || search.calls != 1 {
				t.Fatalf("decision = %+v, searches = %d", d, search.calls)
			}
		})
	}
}

func TestMonitorSeriesHierarchyAndNoEligibleTargets(t *testing.T) {
	svc, st, search, item := newDecisionTest(t, "series")
	if err := st.UpsertEpisodeMonitor(&store.EpisodeMonitor{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1, Monitored: false}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateMediaFile(&store.MediaFile{MediaItemID: item.ID, Path: "/ep2.mkv", FileName: "ep2.mkv", SeasonNumber: ptr(1), EpisodeNumber: ptr(2)}); err != nil {
		t.Fatal(err)
	}
	svc.processItem(item)
	d := latestDecision(t, st, item.ID)
	outcomes := map[string]bool{}
	for _, detail := range d.Details {
		outcomes[detail.Outcome] = true
	}
	if search.calls != 0 || !outcomes["disabled"] || !outcomes["already_present"] || !outcomes["no_eligible_targets"] {
		t.Fatalf("eligibility = %+v, searches = %d", d, search.calls)
	}
	st.fail = "overrides"
	svc.processItem(item)
	if latestDecision(t, st, item.ID).Outcome != "error" || search.calls != 0 {
		t.Fatal("failed override read must not trigger a search")
	}
}

func TestMonitorEpisodeGrabsAndFreshDedup(t *testing.T) {
	svc, st, search, item := newDecisionTest(t, "series")
	search.results = []indexer.TorrentResult{
		{Title: "Show.S01E01.1080p", DownloadURL: "one"},
		{Title: "Show.S01E02.1080p", DownloadURL: "two"},
	}
	search.after = func() {
		if err := st.CreateDownload(&store.Download{MediaItemID: item.ID, Title: "Show.S01E01.1080p", DownloadURL: "manual", Status: "pending"}); err != nil {
			t.Fatal(err)
		}
	}
	svc.processItem(item)
	d := latestDecision(t, st, item.ID)
	if d.Outcome != "grabbed" || !strings.Contains(d.Summary, "Downloads queued: 1") || !strings.Contains(d.Summary, "Checks covered by downloads: 1") {
		t.Fatalf("mixed result = %+v", d)
	}
	downloads, _ := st.ListDownloads(&item.ID, nil)
	if len(downloads) != 2 || item.MonitorSearchStartedAt != nil {
		t.Fatalf("downloads=%+v marker=%v", downloads, item.MonitorSearchStartedAt)
	}
}

func TestMonitorDecisionBoundedAndSafe(t *testing.T) {
	svc, st, _, item := newDecisionTest(t, "movie")
	check := newDecisionCheck(item)
	for i := 1; i <= 200; i++ {
		d := decisionDetail("disabled", "Episode is not monitored.")
		d.EpisodeNumber = ptr(i)
		check.add(d)
	}
	check.add(decisionDetail("grabbed", "Download queued."))
	svc.saveDecision(check)
	d := latestDecision(t, st, item.ID)
	if len(d.Details) != maxDecisionDetails || !d.Truncated || d.Outcome != "grabbed" || !strings.Contains(d.Summary, "Unmonitored targets: 200") {
		t.Fatalf("bounded summary = %+v", d)
	}
	seenGrab := false
	for _, detail := range d.Details {
		seenGrab = seenGrab || detail.Outcome == "grabbed"
	}
	if !seenGrab {
		t.Fatal("important result was lost to truncation")
	}
	for _, title := range []string{privateError.Error(), "Show\nprivate", strings.Repeat("a", 241), "Show token secret"} {
		if safeSelectedTitle(title) != "" {
			t.Fatalf("unsafe title retained: %q", title)
		}
	}
	if safeSelectedTitle("Show.S01E01.1080p-GROUP") == "" {
		t.Fatal("safe title omitted")
	}
}

func TestMonitorPreferredReleaseAndPackThresholdPreserved(t *testing.T) {
	svc, st, search, item := newDecisionTest(t, "series")
	item.PreferredRelease = "PREFERRED"
	search.results = []indexer.TorrentResult{
		{Title: "Show.S01.Complete.1080p-OTHER", DownloadURL: "other"},
		{Title: "Show.S01.Complete.1080p-PREFERRED", DownloadURL: "preferred"},
	}
	svc.processItem(item)
	downloads, _ := st.ListDownloads(&item.ID, nil)
	if len(downloads) != 1 || downloads[0].DownloadURL != "preferred" || downloads[0].EpisodeID != nil {
		t.Fatalf("preferred pack not selected: %+v", downloads)
	}
	ep := store.Episode{SeasonNumber: 1, EpisodeNumber: 1}
	results := []indexer.TorrentResult{{Title: "Show.S01.Complete"}, {Title: "Show.S01E01"}}
	for _, ratio := range []float64{0.69, 0.7} {
		t.Run(fmt.Sprint(ratio), func(t *testing.T) {
			best := svc.findBestForEpisode(results, ep, "prefer_packs", ratio)
			want := "Show.S01E01"
			if ratio >= 0.7 {
				want = "Show.S01.Complete"
			}
			if best == nil || best.Title != want {
				t.Fatalf("best = %+v, want %s", best, want)
			}
		})
	}
}

func TestDisabledItemsKeepPreviousSnapshot(t *testing.T) {
	svc, st, search, item := newDecisionTest(t, "movie")
	svc.processOnce()
	before := latestDecision(t, st, item.ID)
	item.Monitored = false
	if err := st.UpdateMediaItem(item); err != nil {
		t.Fatal(err)
	}
	svc.processOnce()
	after := latestDecision(t, st, item.ID)
	if !after.CheckedAt.Equal(before.CheckedAt) || search.calls != 1 {
		t.Fatal("disabled item should retain its honest previous check, not fabricate a new one")
	}
	if time.Since(after.CheckedAt) > time.Minute {
		t.Fatal("unexpected check timestamp")
	}
}

func TestSeriesRealURLDedupDoesNotClaimGrab(t *testing.T) {
	svc, st, search, item := newDecisionTest(t, "series")
	search.results = []indexer.TorrentResult{{Title: "Show.S01.Complete.1080p", DownloadURL: "same-release"}}
	search.after = func() {
		// A concurrently created row with no parseable episode/season reaches
		// the URL guard instead of the earlier episode-map guard.
		if err := st.CreateDownload(&store.Download{MediaItemID: item.ID, Title: "Manual", DownloadURL: "same-release", Status: "pending"}); err != nil {
			t.Fatal(err)
		}
	}
	svc.processItem(item)
	d := latestDecision(t, st, item.ID)
	downloads, _ := st.ListDownloads(&item.ID, nil)
	if d.Outcome != "active_download" || len(downloads) != 1 || item.MonitorSearchStartedAt == nil {
		t.Fatalf("duplicate was treated as a grab: %+v, downloads=%+v", d, downloads)
	}
}

func TestSeriesEligibilityDetails(t *testing.T) {
	for _, branch := range []string{"no_match", "unaired", "missing_metadata", "active_download", "episode_override"} {
		t.Run(branch, func(t *testing.T) {
			svc, st, search, item := newDecisionTest(t, "series")
			if err := st.DeleteEpisodesByMediaItem(item.ID); err != nil {
				t.Fatal(err)
			}
			ep := &store.Episode{MediaItemID: item.ID, SeasonNumber: 1, EpisodeNumber: 1, AirDate: "2000-01-01"}
			want, searches := branch, 0
			switch branch {
			case "no_match":
				search.results = []indexer.TorrentResult{{Title: "Show.S02E01", DownloadURL: "wrong-season"}}
				searches = 1
			case "unaired":
				ep.AirDate = "2999-01-01"
			case "missing_metadata":
				ep.AirDate = ""
			case "active_download":
				if err := st.CreateDownload(&store.Download{MediaItemID: item.ID, Title: "Show.S01E01", DownloadURL: "existing", Status: "pending"}); err != nil {
					t.Fatal(err)
				}
			case "episode_override":
				ep.SeasonNumber = 2 // no season monitor exists for this season
				if err := st.UpsertEpisodeMonitor(&store.EpisodeMonitor{MediaItemID: item.ID, SeasonNumber: 2, EpisodeNumber: 1, Monitored: true}); err != nil {
					t.Fatal(err)
				}
				want, searches = "no_results", 1
			}
			if err := st.CreateEpisode(ep); err != nil {
				t.Fatal(err)
			}
			svc.processItem(item)
			d := latestDecision(t, st, item.ID)
			if d.Outcome != want || search.calls != searches {
				t.Fatalf("decision = %+v, searches = %d; want %s/%d", d, search.calls, want, searches)
			}
			if d.Details[0].SeasonNumber == nil || *d.Details[0].SeasonNumber != ep.SeasonNumber {
				t.Fatalf("missing target detail: %+v", d.Details)
			}
		})
	}
}

func TestMonitorRealIndexerDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good":
			fmt.Fprint(w, `<table>
<tr><td class="title">Show.S01.Complete.2160p</td><td class="seeds">100</td><td><a href="/4k?apikey=secret">Get</a></td></tr>
<tr><td class="title">Show.S01.Complete.1080p-OTHER</td><td class="seeds">30</td><td><a href="/other?apikey=secret">Get</a></td></tr>
<tr><td class="title">Show.S01.Complete.1080p-PREFERRED</td><td class="seeds">10</td><td><a href="/preferred?apikey=secret">Get</a></td></tr>
</table>`)
		case "/empty":
			fmt.Fprint(w, "<table></table>")
		default:
			http.Error(w, privateError.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	cache := t.TempDir()
	definition := fmt.Sprintf(`
id: test
name: Test
links: [%s]
search:
  path: "{{ .Config.path }}"
  rows:
    selector: tr
  fields:
    title:
      selector: .title
    seeders:
      selector: .seeds
    download:
      selector: a
      attribute: href
`, server.URL)
	if err := os.WriteFile(filepath.Join(cache, "test.yml"), []byte(definition), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, mediaType := range []string{"movie", "series"} {
		for _, tc := range []struct {
			name    string
			paths   []string
			want    string
			partial bool
		}{
			{name: "none", want: "no_indexers"},
			{name: "disabled", paths: []string{"good"}, want: "no_indexers"},
			{name: "all_failed", paths: []string{"failure", "failure"}, want: "indexer_error"},
			{name: "genuine_empty", paths: []string{"empty"}, want: "no_results"},
			{name: "partial_empty", paths: []string{"empty", "failure"}, want: "partial_indexer_failure", partial: true},
			{name: "partial_rejected", paths: []string{"good", "failure"}, want: "partial_indexer_failure", partial: true},
			{name: "partial_grab", paths: []string{"good", "failure"}, want: "grabbed", partial: true},
		} {
			t.Run(mediaType+"/"+tc.name, func(t *testing.T) {
				svc, st, _, item := newDecisionTest(t, mediaType)
				for _, path := range tc.paths {
					idx := &store.Indexer{Name: "private-name", DefinitionID: "test", Enabled: true, Settings: fmt.Sprintf(`{"path":%q}`, path)}
					if err := st.CreateIndexer(idx); err != nil {
						t.Fatal(err)
					}
					if tc.name == "disabled" {
						idx.Enabled = false
						if err := st.UpdateIndexer(idx); err != nil {
							t.Fatal(err)
						}
					}
				}
				profile := &store.MediaProfile{Name: "1080p", Resolutions: `["1080p"]`, Languages: `[]`}
				if tc.name == "partial_rejected" {
					profile.Resolutions = `["720p"]`
				}
				if err := st.CreateMediaProfile(profile); err != nil {
					t.Fatal(err)
				}
				item.MediaProfileID, item.PreferredRelease = &profile.ID, "PREFERRED"
				idxSvc, err := indexer.NewService(st, svc.settings, cache)
				if err != nil {
					t.Fatal(err)
				}
				svc.indexerSvc = idxSvc
				svc.processItem(item)
				d := latestDecision(t, st, item.ID)
				if d.Outcome != tc.want {
					t.Fatalf("snapshot = %+v, want %s", d, tc.want)
				}
				partial := false
				for _, detail := range d.Details {
					if detail.Outcome == "partial_indexer_failure" {
						partial = true
						if !strings.Contains(detail.Explanation, "1 of 2 attempted indexers failed") {
							t.Fatalf("missing safe counts: %+v", detail)
						}
					}
					if tc.name == "partial_grab" && detail.TotalResults != 0 && (detail.TotalResults != 3 || detail.RejectedResults != 1) {
						t.Fatalf("profile counts changed: %+v", detail)
					}
					if tc.name == "all_failed" && !strings.Contains(detail.Explanation, "All 2 attempted indexers failed") {
						t.Fatalf("all failures not explained: %+v", detail)
					}
				}
				if partial != tc.partial || (tc.partial && !strings.Contains(d.Summary, "Searches with partial indexer failures: 1")) {
					t.Fatalf("partial failure missing from snapshot: %+v", d)
				}
				downloads, err := st.ListDownloads(&item.ID, nil)
				if err != nil {
					t.Fatal(err)
				}
				if tc.name == "partial_grab" {
					if len(downloads) != 1 || downloads[0].Title != "Show.S01.Complete.1080p-PREFERRED" {
						t.Fatalf("ranking/preference changed: %+v", downloads)
					}
				} else if len(downloads) != 0 {
					t.Fatalf("unexpected grab: %+v", downloads)
				}
			})
		}
	}
}
