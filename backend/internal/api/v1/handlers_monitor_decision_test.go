package apiv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/store"
)

type monitorDecisionReadStore struct {
	store.Store
	itemErr     error
	decisionErr error
	decision    *store.MonitorDecision
	reads       int
}

func (s *monitorDecisionReadStore) GetMediaItem(id uint) (*store.MediaItem, error) {
	s.reads++
	return &store.MediaItem{ID: id}, s.itemErr
}

func (s *monitorDecisionReadStore) GetMonitorDecision(uint) (*store.MonitorDecision, error) {
	s.reads++
	return s.decision, s.decisionErr
}

func TestGetMonitorDecisionReadOnlyAndMapping(t *testing.T) {
	sn, en, downloadID := 1, 2, uint(123)
	checked := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	input := checked.Add(-time.Hour)
	st := &monitorDecisionReadStore{decision: &store.MonitorDecision{
		MediaItemID: 5, CheckedAt: checked, Outcome: "grabbed", Summary: "1 download queued.", Truncated: true,
		InputUpdatedAt: &input,
		Details: []store.MonitorDecisionDetail{{
			SeasonNumber: &sn, EpisodeNumber: &en, Outcome: "grabbed", Explanation: "Queued, not yet imported.",
			SelectedTitle: "Show.S01E02", DownloadID: &downloadID,
			TotalResults: 12, RejectedResults: 4, BlockedResults: 1,
		}},
	}}
	h := &Handlers{store: st}
	for range 2 {
		response, err := h.GetMonitorDecision(context.Background(), GetMonitorDecisionRequestObject{Id: 5})
		if err != nil {
			t.Fatal(err)
		}
		d := response.(GetMonitorDecision200JSONResponse).Decision
		if d == nil || !d.CheckedAt.Equal(checked) || !d.Truncated || d.Summary != st.decision.Summary || len(d.Details) != 1 {
			t.Fatalf("response = %+v", d)
		}
		if d.InputUpdatedAt == nil || !d.InputUpdatedAt.Equal(input) {
			t.Fatalf("input version not preserved: %+v", d)
		}
		detail := d.Details[0]
		if detail.SeasonNumber == nil || *detail.SeasonNumber != sn || detail.EpisodeNumber == nil || *detail.EpisodeNumber != en || detail.DownloadId == nil || *detail.DownloadId != 123 || detail.SelectedTitle == nil || *detail.SelectedTitle != "Show.S01E02" || detail.TotalResults != 12 || detail.RejectedResults != 4 || detail.BlockedResults != 1 {
			t.Fatalf("detail mapping = %+v", detail)
		}
	}
	if st.reads != 4 {
		t.Fatalf("read count = %d", st.reads)
	}
	// All write/search services are nil: fetching only reads persisted data.
}

func TestGetMonitorDecisionMissingAndFailures(t *testing.T) {
	readError := errors.New("database unavailable")
	for _, tc := range []struct {
		name                 string
		id                   int64
		itemErr, decisionErr error
		wantStatus           int
		wantError            bool
	}{
		{name: "no snapshot", id: 1, decisionErr: store.ErrNotFound, wantStatus: 200},
		{name: "nil snapshot", id: 1, wantStatus: 200},
		{name: "missing item", id: 1, itemErr: store.ErrNotFound, wantStatus: 404},
		{name: "invalid id", id: -1, wantStatus: 404},
		{name: "item read error", id: 1, itemErr: readError, wantError: true},
		{name: "snapshot read error", id: 1, decisionErr: readError, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &monitorDecisionReadStore{itemErr: tc.itemErr, decisionErr: tc.decisionErr}
			h := &Handlers{store: st}
			response, err := h.GetMonitorDecision(context.Background(), GetMonitorDecisionRequestObject{Id: tc.id})
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v", err)
			}
			if tc.wantError {
				return
			}
			w := httptest.NewRecorder()
			if err := response.VisitGetMonitorDecisionResponse(w); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d", w.Code)
			}
			if w.Code == 200 && w.Body.String() != "{}\n" {
				t.Fatalf("fabricated missing snapshot: %s", w.Body.String())
			}
		})
	}
}

func TestMonitorDecisionEndpointAuthenticatedNotAdminOnly(t *testing.T) {
	st := &monitorDecisionReadStore{decision: &store.MonitorDecision{
		CheckedAt: time.Now().UTC(), Outcome: "no_results", Summary: "No results returned.",
	}}
	authSvc := auth.NewService(st, "test-signing-key")
	token, err := authSvc.GenerateAccessToken(&store.User{ID: 7, Email: "reader@example.test", IsAdmin: false})
	if err != nil {
		t.Fatal(err)
	}
	h := &Handlers{store: st}
	routes := HandlerWithOptions(NewStrictHandler(h, []StrictMiddlewareFunc{AdminMiddleware(authSvc)}), StdHTTPServerOptions{BaseURL: "/api/v1"})
	handler := auth.AuthMiddleware(authSvc)(routes)
	for _, authenticated := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/media/5/monitor-decision", nil)
		if authenticated {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if !authenticated {
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("anonymous status = %d", w.Code)
			}
			continue
		}
		if w.Code != http.StatusOK {
			t.Fatalf("non-admin status = %d: %s", w.Code, w.Body.String())
		}
		var response struct {
			Decision MonitorDecision `json:"decision"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Decision.Details == nil || len(response.Decision.Details) != 0 {
			t.Fatalf("details must be []: %s", w.Body.String())
		}
		if response.Decision.InputUpdatedAt != nil {
			t.Fatalf("legacy snapshot must have unknown input freshness: %s", w.Body.String())
		}
	}
}
