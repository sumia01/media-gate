package apiv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sumia01/media-gate/internal/store"
	syncsvc "github.com/sumia01/media-gate/internal/sync"
)

type timelineHandlerStore struct {
	store.Store
	rows     []store.TimelineEpisode
	err      error
	from, to string
}

func (s *timelineHandlerStore) ListTimelineEpisodes(from, to string) ([]store.TimelineEpisode, error) {
	s.from, s.to = from, to
	return s.rows, s.err
}

func (s *timelineHandlerStore) ListDownloads(_ *uint, _ *string) ([]store.Download, error) {
	season := 1
	return []store.Download{{SeasonNumber: &season, Title: "Show.S01E02", Status: "downloading"}}, nil
}

func TestGetEpisodeTimelineValidation(t *testing.T) {
	for _, dates := range [][2]string{
		{"", "2026-09-05"}, {"2026-09-05", ""}, {"2026-9-05", "2026-09-06"},
		{"2026-02-30", "2026-03-02"}, {"2026-09-05T00:00:00Z", "2026-09-06"},
		{"2026-09-05", "2026-09-05"}, {"2026-09-06", "2026-09-05"},
		{"2026-01-01", "2026-02-02"}, {"2026-09-05", "2026-13-01"},
	} {
		t.Run(dates[0]+"/"+dates[1], func(t *testing.T) {
			// No service: invalid requests must return before any data access.
			res, err := (&Handlers{}).GetEpisodeTimeline(context.Background(), GetEpisodeTimelineRequestObject{Params: GetEpisodeTimelineParams{From: dates[0], To: dates[1]}})
			if err != nil {
				t.Fatal(err)
			}
			bad, ok := res.(GetEpisodeTimeline400JSONResponse)
			if !ok || bad.Code != 400 || bad.Message == "" {
				t.Fatalf("got %#v", res)
			}
		})
	}
	for _, dates := range [][2]string{{"2026-01-01", "2026-02-01"}, {"2024-02-29", "2024-03-01"}, {"2026-12-31", "2027-01-01"}} {
		fake := &timelineHandlerStore{}
		res, err := (&Handlers{syncSvc: syncsvc.NewService(fake)}).GetEpisodeTimeline(context.Background(), GetEpisodeTimelineRequestObject{Params: GetEpisodeTimelineParams{From: dates[0], To: dates[1]}})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := res.(GetEpisodeTimeline200JSONResponse); !ok || fake.from != dates[0] || fake.to != dates[1] {
			t.Fatalf("got %#v", res)
		}
		recorder := httptest.NewRecorder()
		if err := res.VisitGetEpisodeTimelineResponse(recorder); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != 200 || recorder.Body.String() != "{\"items\":[]}\n" {
			t.Fatalf("empty response: %s", recorder.Body.String())
		}
	}
}

func TestGetEpisodeTimelineEnrichedResponse(t *testing.T) {
	runtime := 45
	fake := &timelineHandlerStore{}
	for i := range 2 {
		fake.rows = append(fake.rows, store.TimelineEpisode{
			Episode:     store.Episode{ID: uint(i + 1), MediaItemID: 7, SeasonNumber: 1, EpisodeNumber: i + 1, AirDate: "2026-09-05", Title: "Episode", Overview: "Overview", Runtime: &runtime},
			SeriesTitle: "Series", PosterPath: "/poster.jpg", HasFile: i == 0, Monitored: i != 0,
		})
	}
	h := &Handlers{syncSvc: syncsvc.NewService(fake)}
	res, err := h.GetEpisodeTimeline(context.Background(), GetEpisodeTimelineRequestObject{Params: GetEpisodeTimelineParams{From: "2026-09-01", To: "2026-09-15"}})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if err := res.VisitGetEpisodeTimelineResponse(recorder); err != nil {
		t.Fatal(err)
	}
	var body GetEpisodeTimeline200JSONResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("got %+v", body)
	}
	for i, item := range body.Items {
		ep := item.Episode
		if item.SeriesTitle != "Series" || item.PosterPath == nil || *item.PosterPath != "/poster.jpg" || ep.Id != int64(i+1) || ep.MediaItemId != 7 || ep.HasFile == nil || *ep.HasFile != (i == 0) || ep.Monitored == nil || *ep.Monitored != (i != 0) || ep.AirDate == nil || *ep.AirDate != "2026-09-05" || ep.Title == nil || ep.Overview == nil || ep.Runtime == nil || *ep.Runtime != 45 {
			t.Errorf("item %d: %+v", i, item)
		}
	}
	if body.Items[0].Episode.DownloadStatus != nil || body.Items[1].Episode.DownloadStatus == nil || *body.Items[1].Episode.DownloadStatus != "downloading" {
		t.Fatalf("single-episode status leaked: %+v", body.Items)
	}
	fake.err = errors.New("storage failed")
	if _, err := h.GetEpisodeTimeline(context.Background(), GetEpisodeTimelineRequestObject{Params: GetEpisodeTimelineParams{From: "2026-09-01", To: "2026-09-15"}}); !errors.Is(err, fake.err) {
		t.Fatalf("got error %v", err)
	}
}

func TestGetEpisodeTimelineHTTP(t *testing.T) {
	fake := &timelineHandlerStore{}
	handler := HandlerWithOptions(NewStrictHandler(&Handlers{syncSvc: syncsvc.NewService(fake)}, nil), StdHTTPServerOptions{ErrorHandlerFunc: RequestErrorHandler})
	for _, tc := range []struct {
		query string
		code  int
	}{
		{"from=2026-09-01&to=2026-09-15", http.StatusOK},
		{"from=invalid&to=2026-09-15", http.StatusBadRequest},
		{"from=2026-09-01&to=2026-09-01", http.StatusBadRequest},
		{"from=2026-09-01&to=2026-10-03", http.StatusBadRequest},
		{"from=2026-09-01", http.StatusBadRequest},
		{"to=2026-09-15", http.StatusBadRequest},
		{"", http.StatusBadRequest},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/media/episode-timeline?"+tc.query, nil))
		if recorder.Code != tc.code || recorder.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("%s: status %d, body %s", tc.query, recorder.Code, recorder.Body.String())
		}
		if tc.code == http.StatusBadRequest {
			var body ErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Code != tc.code || body.Message == "" {
				t.Fatalf("bad request body: %+v, %v", body, err)
			}
		}
	}
}
