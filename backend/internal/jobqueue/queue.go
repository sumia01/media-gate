package jobqueue

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	"github.com/sumia01/media-gate/internal/eventbus"
	"github.com/sumia01/media-gate/internal/matching"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/sync"
)

type JobType string

const (
	JobTypeSyncLibrary  JobType = "sync_library"
	JobTypeMatchLibrary JobType = "match_library"
)

var (
	ErrFull    = errors.New("job queue is full; try again later")
	ErrStopped = errors.New("job queue is shutting down")
)

type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
)

type JobProgress struct {
	Current int
	Total   int
	Message string
}

type Job struct {
	ID          string
	Type        JobType
	LibraryID   uint
	LibraryName string
	Status      JobStatus
	Progress    *JobProgress
	Error       string
	FullRematch bool
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}

type Queue struct {
	mu       gosync.Mutex
	jobs     map[string]*Job
	pending  chan *Job
	syncSvc  *sync.Service
	matchSvc *matching.Service
	store    store.Store
	bus      *eventbus.Bus
	stopCh   chan struct{}
	doneCh   chan struct{}
	started  bool
	stopping bool
	seq      int
}

func New(syncSvc *sync.Service, matchSvc *matching.Service, s store.Store, bufSize int, bus *eventbus.Bus) *Queue {
	q := &Queue{
		jobs:     make(map[string]*Job),
		pending:  make(chan *Job, bufSize),
		syncSvc:  syncSvc,
		matchSvc: matchSvc,
		store:    s,
		bus:      bus,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}

	maxID, err := s.MaxJobRecordID()
	if err != nil {
		slog.Warn("failed to read max job record ID, starting from 0", "error", err)
	} else {
		q.seq = int(maxID)
	}

	return q
}

func (q *Queue) Start() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.started || q.stopping {
		return
	}
	q.started = true
	go q.worker()
}

// BeginShutdown rejects new submissions and discards waiting jobs without
// interrupting the active job. It does not wait for that job to finish.
func (q *Queue) BeginShutdown() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopping {
		return
	}
	q.stopping = true
	close(q.stopCh)
	if !q.started {
		close(q.doneCh)
	}
	for id, job := range q.jobs {
		if job.Status == JobStatusPending {
			delete(q.jobs, id)
		}
	}
	for {
		select {
		case <-q.pending:
		default:
			return
		}
	}
}

// Stop closes admission and waits for the active job, including its history
// write and final events. Safe to call repeatedly; the queue cannot restart.
func (q *Queue) Stop() {
	q.BeginShutdown()
	<-q.doneCh
}

type EnqueueOpts struct {
	FullRematch bool
}

func (q *Queue) Enqueue(typ JobType, libID uint, libName string, opts ...EnqueueOpts) (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopping {
		return nil, ErrStopped
	}

	// Check for existing pending/running job of the same type for this library
	for _, j := range q.jobs {
		if j.LibraryID == libID && j.Type == typ && (j.Status == JobStatusPending || j.Status == JobStatusRunning) {
			return nil, fmt.Errorf("library %d already has an active %s job", libID, typ)
		}
	}

	job := &Job{
		ID:          fmt.Sprintf("job_%d", q.seq+1),
		Type:        typ,
		LibraryID:   libID,
		LibraryName: libName,
		Status:      JobStatusPending,
		CreatedAt:   time.Now(),
	}
	if len(opts) > 0 {
		job.FullRematch = opts[0].FullRematch
	}

	select {
	case q.pending <- job:
		q.seq++
		q.jobs[job.ID] = job
		return snapshotJob(job), nil
	default:
		return nil, ErrFull
	}
}

// ListJobs returns caller-owned snapshots, including nested progress/timestamps.
func (q *Queue) ListJobs() []*Job {
	q.mu.Lock()
	defer q.mu.Unlock()

	var result []*Job
	// Active jobs first
	for _, j := range q.jobs {
		if j.Status == JobStatusPending || j.Status == JobStatusRunning {
			result = append(result, snapshotJob(j))
		}
	}
	// Then completed/failed from DB
	records, err := q.store.ListJobRecords(50)
	if err != nil {
		slog.Error("failed to list job records from DB", "error", err)
		return result
	}
	for _, r := range records {
		result = append(result, recordToJob(&r))
	}
	return result
}

func (q *Queue) worker() {
	defer close(q.doneCh)
	for {
		select {
		case <-q.stopCh:
			return
		case job := <-q.pending:
			q.execute(job)
		}
	}
}

func (q *Queue) execute(job *Job) {
	q.mu.Lock()
	// Receiving a queued job is not a claim: shutdown and the transition to
	// running must share the same lock so no new work starts after admission closes.
	if q.stopping {
		q.mu.Unlock()
		return
	}
	now := time.Now()
	job.Status = JobStatusRunning
	job.StartedAt = &now
	q.mu.Unlock()

	lib, err := q.store.GetLibrary(job.LibraryID)
	if err != nil {
		q.finishJob(job, "", err)
		return
	}

	switch job.Type {
	case JobTypeSyncLibrary:
		q.bus.Publish(eventbus.LibrarySyncStarted, eventbus.LibrarySyncPayload{
			LibraryID: lib.ID, LibraryName: lib.Name,
		})
		added, removed, syncErr := q.syncSvc.SyncLibrary(lib)
		if syncErr != nil {
			q.finishJob(job, "", syncErr)
			q.bus.Publish(eventbus.LibrarySyncFailed, eventbus.LibrarySyncPayload{
				LibraryID: lib.ID, LibraryName: lib.Name,
			})
			return
		}
		msg := fmt.Sprintf("added %d, removed %d", added, removed)
		q.finishJob(job, msg, nil)
		q.bus.Publish(eventbus.LibrarySyncCompleted, eventbus.LibrarySyncPayload{
			LibraryID: lib.ID, LibraryName: lib.Name, Added: added, Removed: removed,
		})

		// Auto-enqueue matching after sync
		if q.matchSvc != nil {
			if _, enqErr := q.Enqueue(JobTypeMatchLibrary, lib.ID, lib.Name); enqErr != nil {
				slog.Debug("skipping auto-match enqueue", "reason", enqErr)
			}
		}

	case JobTypeMatchLibrary:
		q.bus.Publish(eventbus.LibraryMatchStarted, eventbus.LibraryMatchPayload{
			LibraryID: lib.ID, LibraryName: lib.Name,
		})
		progressFn := func(current, total int, itemID uint) {
			q.mu.Lock()
			job.Progress = &JobProgress{
				Current: current,
				Total:   total,
				Message: fmt.Sprintf("matching %d/%d", current, total),
			}
			q.mu.Unlock()
			q.bus.Publish(eventbus.LibraryMatchProgress, eventbus.LibraryMatchPayload{
				LibraryID: lib.ID, LibraryName: lib.Name, Current: current, Total: total, MediaItemID: itemID,
			})
		}
		matchErr := q.matchSvc.MatchLibrary(lib, job.FullRematch, progressFn)
		if matchErr != nil {
			q.finishJob(job, "", matchErr)
			q.bus.Publish(eventbus.LibraryMatchFailed, eventbus.LibraryMatchPayload{
				LibraryID: lib.ID, LibraryName: lib.Name,
			})
			return
		}
		q.finishJob(job, "matching complete", nil)
		q.bus.Publish(eventbus.LibraryMatchCompleted, eventbus.LibraryMatchPayload{
			LibraryID: lib.ID, LibraryName: lib.Name,
		})
	}
}

func (q *Queue) finishJob(job *Job, message string, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now()
	job.CompletedAt = &now

	if err != nil {
		job.Status = JobStatusFailed
		job.Error = err.Error()
		slog.Error("job failed", "job_id", job.ID, "error", err)
	} else {
		job.Status = JobStatusCompleted
		job.Progress = &JobProgress{Message: message}
		slog.Info("job completed", "job_id", job.ID, "result", message)
	}

	// Persist to DB
	record := &store.JobRecord{
		ID:            jobIDToUint(job.ID),
		Type:          string(job.Type),
		LibraryID:     job.LibraryID,
		LibraryName:   job.LibraryName,
		Status:        string(job.Status),
		ResultMessage: message,
		Error:         job.Error,
		CreatedAt:     job.CreatedAt,
		StartedAt:     job.StartedAt,
		CompletedAt:   job.CompletedAt,
	}
	if dbErr := q.store.CreateJobRecord(record); dbErr != nil {
		slog.Error("failed to persist job record", "job_id", job.ID, "error", dbErr)
	} else {
		if trimErr := q.store.DeleteOldJobRecords(200); trimErr != nil {
			slog.Warn("failed to trim old job records", "error", trimErr)
		}
	}

	// Remove from active map
	delete(q.jobs, job.ID)
}

func recordToJob(r *store.JobRecord) *Job {
	j := &Job{
		ID:          fmt.Sprintf("job_%d", r.ID),
		Type:        JobType(r.Type),
		LibraryID:   r.LibraryID,
		LibraryName: r.LibraryName,
		Status:      JobStatus(r.Status),
		Error:       r.Error,
		CreatedAt:   r.CreatedAt,
		StartedAt:   r.StartedAt,
		CompletedAt: r.CompletedAt,
	}
	if r.ResultMessage != "" {
		j.Progress = &JobProgress{Message: r.ResultMessage}
	}
	return snapshotJob(j)
}

func snapshotJob(job *Job) *Job {
	snapshot := *job
	if job.Progress != nil {
		progress := *job.Progress
		snapshot.Progress = &progress
	}
	if job.StartedAt != nil {
		started := *job.StartedAt
		snapshot.StartedAt = &started
	}
	if job.CompletedAt != nil {
		completed := *job.CompletedAt
		snapshot.CompletedAt = &completed
	}
	return &snapshot
}

func jobIDToUint(id string) uint {
	s := strings.TrimPrefix(id, "job_")
	n, _ := strconv.ParseUint(s, 10, 64)
	return uint(n)
}
