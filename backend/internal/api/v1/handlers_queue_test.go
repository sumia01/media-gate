package apiv1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sumia01/media-gate/internal/jobqueue"
	"github.com/sumia01/media-gate/internal/library"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
)

func TestLibraryJobAdmissionResponses(t *testing.T) {
	for _, operation := range []string{"sync", "match"} {
		for _, tc := range []struct {
			name    string
			status  int
			message string
		}{
			{name: "accepted", status: http.StatusAccepted},
			{name: "duplicate", status: http.StatusConflict, message: "already has an active"},
			{name: "full", status: http.StatusServiceUnavailable, message: jobqueue.ErrFull.Error()},
			{name: "shutdown", status: http.StatusServiceUnavailable, message: jobqueue.ErrStopped.Error()},
			{name: "missing", status: http.StatusNotFound, message: "library not found"},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				st, err := sqlite.New(filepath.Join(t.TempDir(), "admission.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = st.Close() })
				lib := &store.Library{Name: "Admission", Path: t.TempDir(), MediaType: "movie"}
				if err := st.CreateLibrary(lib); err != nil {
					t.Fatal(err)
				}
				q := jobqueue.New(nil, nil, st, 1, nil)
				t.Cleanup(q.Stop) // No worker starts; these tests only exercise admission.
				typ := jobqueue.JobTypeSyncLibrary
				if operation == "match" {
					typ = jobqueue.JobTypeMatchLibrary
				}
				id := lib.ID
				switch tc.name {
				case "duplicate":
					if _, err := q.Enqueue(typ, id, lib.Name); err != nil {
						t.Fatal(err)
					}
				case "full":
					if _, err := q.Enqueue(typ, id+1, "Another library"); err != nil {
						t.Fatal(err)
					}
				case "shutdown":
					q.BeginShutdown()
				case "missing":
					id++
				}
				h := &Handlers{lib: library.NewService(st, nil, nil), queue: q}
				recorder := httptest.NewRecorder()
				if operation == "sync" {
					response, err := h.TriggerSync(context.Background(), TriggerSyncRequestObject{Id: int64(id)})
					if err != nil {
						t.Fatal(err)
					}
					if err := response.VisitTriggerSyncResponse(recorder); err != nil {
						t.Fatal(err)
					}
				} else {
					full := true
					response, err := h.TriggerMatch(context.Background(), TriggerMatchRequestObject{
						Id: int64(id), Params: TriggerMatchParams{FullRematch: &full},
					})
					if err != nil {
						t.Fatal(err)
					}
					if err := response.VisitTriggerMatchResponse(recorder); err != nil {
						t.Fatal(err)
					}
				}
				if recorder.Code != tc.status {
					t.Fatalf("status=%d, want %d: %s", recorder.Code, tc.status, recorder.Body)
				}
				if tc.status == http.StatusAccepted {
					var job Job
					if err := json.Unmarshal(recorder.Body.Bytes(), &job); err != nil {
						t.Fatal(err)
					}
					if job.Id == "" || string(job.Status) != "pending" || string(job.Type) != string(typ) {
						t.Fatalf("invalid accepted snapshot: %+v", job)
					}
					if operation == "match" && !q.ListJobs()[0].FullRematch {
						t.Fatal("full-rematch option was lost")
					}
				} else {
					var response ErrorResponse
					if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
						t.Fatal(err)
					}
					if response.Code != tc.status || !strings.Contains(response.Message, tc.message) {
						t.Fatalf("unexpected rejection: %+v", response)
					}
				}
			})
		}
	}
}
