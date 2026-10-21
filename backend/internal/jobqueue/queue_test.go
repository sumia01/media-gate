package jobqueue

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/matching"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/store/sqlite"
	mediasync "github.com/sumia01/media-gate/internal/sync"
)

// Unimplemented Store methods panic rather than silently accepting unexpected
// calls. Real SQLite coverage below verifies the persistence/shutdown composition.
type queueStore struct {
	store.Store
	maxID        uint
	lookups      []uint
	records      []store.JobRecord
	getLibrary   func(uint) (*store.Library, error)
	beforeRecord func()
}

func (s *queueStore) MaxJobRecordID() (uint, error) { return s.maxID, nil }
func (s *queueStore) GetLibrary(id uint) (*store.Library, error) {
	s.lookups = append(s.lookups, id)
	if s.getLibrary != nil {
		return s.getLibrary(id)
	}
	return nil, store.ErrNotFound
}
func (s *queueStore) ListJobRecords(_ int) ([]store.JobRecord, error) { return s.records, nil }
func (s *queueStore) CreateJobRecord(record *store.JobRecord) error {
	if s.beforeRecord != nil {
		s.beforeRecord()
	}
	s.records = append(s.records, *record)
	return nil
}
func (s *queueStore) DeleteOldJobRecords(_ int) error { return nil }
func (s *queueStore) ListMediaFilesByLibrary(_ uint) ([]store.MediaFile, error) {
	return nil, nil // Empty library fixtures have no files or mutations.
}

func stopQueue(t *testing.T, q *Queue) {
	t.Helper()
	done := make(chan struct{})
	go func() { q.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("queue Stop did not finish after releasing test gates")
	}
}

func testQueue(t *testing.T, s *queueStore, capacity int) *Queue {
	t.Helper()
	bus := eventbus.New(32)
	bus.Start()
	t.Cleanup(bus.Stop)
	q := New(nil, nil, s, capacity, bus)
	t.Cleanup(func() {
		if t.Failed() {
			// Release a sender if a regression blocked it on capacity while
			// holding q.mu, so failure cleanup can still acquire the mutex.
			select {
			case <-q.pending:
			default:
			}
		}
		stopQueue(t, q)
	})
	return q
}

func requireEnqueue(t *testing.T, q *Queue, typ JobType, libID uint) *Job {
	t.Helper()
	job, err := q.Enqueue(typ, libID, fmt.Sprintf("Library %d", libID))
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func requireSignal(t *testing.T, ch <-chan struct{}, wantClosed bool, description string) {
	t.Helper()
	select {
	case <-ch:
		if !wantClosed {
			t.Fatalf("%s completed too early", description)
		}
	default:
		if wantClosed {
			t.Fatalf("%s did not complete", description)
		}
	}
}

func TestQueueShutdownFinishesOnlyActiveJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		library := &store.Library{ID: 1, Name: "Active", Path: t.TempDir()}
		lookupGate, recordGate := make(chan struct{}), make(chan struct{})
		releaseLookup := sync.OnceFunc(func() { close(lookupGate) })
		releaseRecord := sync.OnceFunc(func() { close(recordGate) })
		defer releaseLookup()
		defer releaseRecord()
		recordStarted := make(chan struct{})
		s := &queueStore{
			getLibrary: func(uint) (*store.Library, error) { <-lookupGate; return library, nil },
			beforeRecord: func() {
				close(recordStarted)
				<-recordGate
			},
		}
		q := testQueue(t, s, 2)
		q.syncSvc = mediasync.NewService(s)
		q.matchSvc = &matching.Service{} // Shutdown must reject the sync-to-match follow-up.
		q.Start()
		active := requireEnqueue(t, q, JobTypeSyncLibrary, 1)
		synctest.Wait()
		requireEnqueue(t, q, JobTypeSyncLibrary, 2)
		requireEnqueue(t, q, JobTypeMatchLibrary, 3)
		q.BeginShutdown()
		if _, err := q.Enqueue(JobTypeSyncLibrary, 1, "duplicate during shutdown"); !errors.Is(err, ErrStopped) {
			t.Fatalf("shutdown submission error = %v, want ErrStopped", err)
		}
		jobs := q.ListJobs()
		if len(jobs) != 1 || jobs[0].ID != active.ID || jobs[0].Status != JobStatusRunning {
			t.Fatalf("shutdown should retain only the active job: %+v", jobs)
		}
		stopped := []chan struct{}{make(chan struct{}), make(chan struct{})}
		for _, done := range stopped {
			go func() { q.Stop(); close(done) }()
		}
		synctest.Wait()
		for _, done := range stopped {
			requireSignal(t, done, false, "Stop during active work")
		}
		releaseLookup()
		synctest.Wait()
		requireSignal(t, recordStarted, true, "history write")
		for _, done := range stopped {
			requireSignal(t, done, false, "Stop during history write")
		}
		releaseRecord()
		synctest.Wait()
		for _, done := range stopped {
			requireSignal(t, done, true, "Stop")
		}
		if len(s.lookups) != 1 || len(s.records) != 1 || s.records[0].Status != string(JobStatusCompleted) {
			t.Fatalf("waiting/follow-up jobs ran or active history was lost: lookups=%v records=%+v", s.lookups, s.records)
		}
		if len(q.pending) != 0 || len(q.jobs) != 0 {
			t.Fatal("shutdown retained pending jobs")
		}
	})
}

func TestQueueShutdownPreventsADequeuedJobFromStarting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &queueStore{}
		q := testQueue(t, s, 1)
		requireEnqueue(t, q, JobTypeSyncLibrary, 1)
		job := <-q.pending // Model receipt just before shutdown wins the claim lock.
		q.BeginShutdown()
		q.execute(job)
		if len(s.lookups) != 0 || len(s.records) != 0 || len(q.ListJobs()) != 0 {
			t.Fatal("a dequeued but unclaimed job executed after shutdown")
		}
		q.Start() // A stopped queue must not restart.
		synctest.Wait()
		if q.started {
			t.Fatal("Start restarted a stopped queue")
		}
	})
}

func TestQueueShutdownSerializesConcurrentSubmissions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &queueStore{}
		q := testQueue(t, s, 32)
		results := make(chan error, 32)
		for id := uint(1); id <= 32; id++ {
			go func() {
				_, err := q.Enqueue(JobTypeSyncLibrary, id, "Concurrent submission")
				results <- err
			}()
		}
		go q.BeginShutdown()
		synctest.Wait()
		accepted := 0
		for range 32 {
			err := <-results
			if err == nil {
				accepted++
			} else if !errors.Is(err, ErrStopped) {
				t.Fatalf("unexpected concurrent rejection: %v", err)
			}
		}
		if q.seq != accepted || len(q.ListJobs()) != 0 || len(q.pending) != 0 {
			t.Fatal("shutdown left a racing submission queued or reserved a rejected ID")
		}
		if _, err := q.Enqueue(JobTypeSyncLibrary, 33, "Too late"); !errors.Is(err, ErrStopped) {
			t.Fatalf("submission after shutdown = %v, want ErrStopped", err)
		}
	})
}

func TestQueueFullRejectsWithoutReservingJobOrID(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &queueStore{maxID: 41}
		q := testQueue(t, s, 1)
		first := requireEnqueue(t, q, JobTypeSyncLibrary, 1)
		errorsCh := make(chan error, 1)
		go func() { _, err := q.Enqueue(JobTypeSyncLibrary, 2, "Full"); errorsCh <- err }()
		synctest.Wait()
		select {
		case err := <-errorsCh:
			if !errors.Is(err, ErrFull) {
				t.Fatalf("full submission error = %v, want ErrFull", err)
			}
		default:
			t.Fatal("enqueue blocked on a full queue")
		}
		if first.ID != "job_42" || len(q.ListJobs()) != 1 || q.seq != 42 {
			t.Fatalf("rejected enqueue changed queue state: first=%+v seq=%d", first, q.seq)
		}
		if _, err := q.Enqueue(JobTypeSyncLibrary, 1, "duplicate"); err == nil || errors.Is(err, ErrFull) {
			t.Fatalf("duplicate must keep its existing conflict error: %v", err)
		}
		q.Start()
		q.Start() // Only one worker may consume jobs.
		synctest.Wait()
		second := requireEnqueue(t, q, JobTypeSyncLibrary, 2)
		synctest.Wait()
		if second.ID != "job_43" || len(s.records) != 2 {
			t.Fatalf("rejected job leaked a reservation: second=%+v records=%+v", second, s.records)
		}
	})
}

func TestQueueAutoMatchDoesNotBlockWhenFull(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(fmt.Sprintf("full=%v", full), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				library := &store.Library{ID: 1, Path: t.TempDir()}
				gate := make(chan struct{})
				release := sync.OnceFunc(func() { close(gate) })
				defer release()
				s := &queueStore{}
				s.getLibrary = func(uint) (*store.Library, error) {
					if len(s.lookups) == 1 {
						<-gate
						return library, nil
					}
					return nil, store.ErrNotFound
				}
				q := testQueue(t, s, 1)
				q.syncSvc = mediasync.NewService(s)
				q.matchSvc = &matching.Service{}
				q.Start()
				requireEnqueue(t, q, JobTypeSyncLibrary, 1)
				synctest.Wait()
				if full {
					requireEnqueue(t, q, JobTypeSyncLibrary, 2)
				}
				release()
				synctest.Wait()
				if len(s.records) != 2 || s.records[0].Status != string(JobStatusCompleted) {
					t.Fatalf("sync/follow-up did not finish: %+v", s.records)
				}
				wantType := JobTypeMatchLibrary
				if full {
					wantType = JobTypeSyncLibrary
				}
				if s.records[1].Type != string(wantType) {
					t.Fatalf("unexpected job after sync: %+v", s.records[1])
				}
			})
		})
	}
}

func TestQueueReturnsIndependentSnapshots(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		s := &queueStore{records: []store.JobRecord{{ID: 99, Status: "completed", StartedAt: &now, CompletedAt: &now}}}
		q := testQueue(t, s, 1)
		accepted, err := q.Enqueue(JobTypeMatchLibrary, 1, "Original", EnqueueOpts{FullRematch: true})
		if err != nil {
			t.Fatal(err)
		}
		accepted.LibraryName = "caller edit"
		accepted.FullRematch = false
		internal := q.jobs[accepted.ID]
		if internal.LibraryName != "Original" || !internal.FullRematch {
			t.Fatal("Enqueue returned the queue's mutable job")
		}
		internal.Progress = &JobProgress{Current: 1, Total: 2, Message: "original"}
		internal.StartedAt, internal.CompletedAt = &now, &now
		jobs := q.ListJobs()
		jobs[0].LibraryName = "changed"
		jobs[0].Progress.Current = 999
		*jobs[0].StartedAt = now.Add(time.Hour)
		*jobs[0].CompletedAt = now.Add(2 * time.Hour)
		*jobs[1].StartedAt = now.Add(3 * time.Hour)
		*jobs[1].CompletedAt = now.Add(4 * time.Hour)
		if internal.LibraryName != "Original" || internal.Progress.Current != 1 || !internal.StartedAt.Equal(now) || !internal.CompletedAt.Equal(now) {
			t.Fatalf("ListJobs leaked active-job pointers: %+v", internal)
		}
		if !s.records[0].StartedAt.Equal(now) || !s.records[0].CompletedAt.Equal(now) {
			t.Fatal("ListJobs leaked history timestamps")
		}
		internal.Progress.Current = 2
		if jobs[0].Progress.Current != 999 || accepted.Status != JobStatusPending {
			t.Fatal("an earlier snapshot changed with queue state")
		}
	})
}

func TestQueueSnapshotsWhileProgressChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := make(chan struct{})
		defer close(gate)
		s := &queueStore{getLibrary: func(uint) (*store.Library, error) { <-gate; return nil, store.ErrNotFound }}
		q := testQueue(t, s, 1)
		q.Start()
		accepted := requireEnqueue(t, q, JobTypeMatchLibrary, 1)
		synctest.Wait()
		var readers sync.WaitGroup
		for range 4 {
			readers.Go(func() {
				for range 100 {
					jobs := q.ListJobs()
					if _, err := json.Marshal(jobs); err != nil {
						t.Error(err)
					}
					jobs[0].LibraryName = "caller-owned"
					if jobs[0].Progress != nil {
						jobs[0].Progress.Current = -1
					}
				}
			})
		}
		go func() {
			for i := 1; i <= 100; i++ {
				q.mu.Lock()
				q.jobs[accepted.ID].Progress = &JobProgress{Current: i, Total: 100}
				q.mu.Unlock()
			}
		}()
		synctest.Wait()
		readers.Wait()
		job := q.ListJobs()[0]
		if job.LibraryName != "Library 1" || job.Progress.Current != 100 {
			t.Fatalf("snapshot consumers changed queue state: %+v", job)
		}
	})
}

type gatedLibraryStore struct {
	store.Store
	started chan struct{}
	release chan struct{}
}

func (s *gatedLibraryStore) GetLibrary(id uint) (*store.Library, error) {
	close(s.started)
	<-s.release
	return s.Store.GetLibrary(id)
}

func TestQueueShutdownPersistsBeforeBusAndDatabaseClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.db")
	st, err := sqlite.New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	library := &store.Library{Name: "Shutdown", Path: t.TempDir(), MediaType: "movie"}
	if err := st.CreateLibrary(library); err != nil {
		t.Fatal(err)
	}
	wrapped := &gatedLibraryStore{Store: st, started: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(wrapped.release) })
	defer release()
	bus := eventbus.New(16)
	var eventErr error
	var events int
	bus.Subscribe(eventbus.LibrarySyncCompleted, func(eventbus.Event) {
		events++
		records, err := st.ListJobRecords(50)
		if err != nil || len(records) != 1 || records[0].Status != "completed" {
			eventErr = fmt.Errorf("completion event before durable history: records=%+v err=%v", records, err)
		}
	})
	bus.Start()
	t.Cleanup(bus.Stop)
	q := New(mediasync.NewService(st), nil, wrapped, 1, bus)
	t.Cleanup(func() { stopQueue(t, q) })
	q.Start()
	first := requireEnqueue(t, q, JobTypeSyncLibrary, library.ID)
	select {
	case <-wrapped.started:
	case <-time.After(5 * time.Second):
		t.Fatal("sync did not start")
	}
	requireEnqueue(t, q, JobTypeSyncLibrary, library.ID+1)
	q.BeginShutdown()
	release()
	stopQueue(t, q)
	bus.Stop()
	if events != 1 || eventErr != nil {
		t.Fatalf("events=%d, err=%v", events, eventErr)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	records, err := reopened.ListJobRecords(50)
	if err != nil || len(records) != 1 || fmt.Sprintf("job_%d", records[0].ID) != first.ID || records[0].CompletedAt == nil {
		t.Fatalf("shutdown history did not survive restart: records=%+v err=%v", records, err)
	}
}
