package jobqueue

import (
	"fmt"
	"log/slog"
	gosync "sync"
	"time"

	"github.com/sumia01/media-gate/internal/matching"
	"github.com/sumia01/media-gate/internal/store"
	"github.com/sumia01/media-gate/internal/sync"
)

type JobType string

const (
	JobTypeSyncLibrary  JobType = "sync_library"
	JobTypeMatchLibrary JobType = "match_library"
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
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}

type Queue struct {
	mu       gosync.Mutex
	jobs     map[string]*Job
	recent   []*Job
	pending  chan *Job
	syncSvc  *sync.Service
	matchSvc *matching.Service
	store    store.Store
	done     chan struct{}
	seq      int
}

func New(syncSvc *sync.Service, matchSvc *matching.Service, s store.Store, bufSize int) *Queue {
	return &Queue{
		jobs:     make(map[string]*Job),
		pending:  make(chan *Job, bufSize),
		syncSvc:  syncSvc,
		matchSvc: matchSvc,
		store:    s,
		done:     make(chan struct{}),
	}
}

func (q *Queue) Start() {
	go q.worker()
}

func (q *Queue) Stop() {
	close(q.done)
}

func (q *Queue) Enqueue(typ JobType, libID uint, libName string) (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Check for existing pending/running job of the same type for this library
	for _, j := range q.jobs {
		if j.LibraryID == libID && j.Type == typ && (j.Status == JobStatusPending || j.Status == JobStatusRunning) {
			return nil, fmt.Errorf("library %d already has an active %s job", libID, typ)
		}
	}

	q.seq++
	job := &Job{
		ID:          fmt.Sprintf("job_%d", q.seq),
		Type:        typ,
		LibraryID:   libID,
		LibraryName: libName,
		Status:      JobStatusPending,
		CreatedAt:   time.Now(),
	}

	q.jobs[job.ID] = job
	q.pending <- job

	return job, nil
}

func (q *Queue) ListJobs() []*Job {
	q.mu.Lock()
	defer q.mu.Unlock()

	var result []*Job
	// Active jobs first
	for _, j := range q.jobs {
		if j.Status == JobStatusPending || j.Status == JobStatusRunning {
			result = append(result, j)
		}
	}
	// Then recent completed/failed
	result = append(result, q.recent...)
	return result
}

func (q *Queue) worker() {
	for {
		select {
		case <-q.done:
			return
		case job := <-q.pending:
			q.execute(job)
		}
	}
}

func (q *Queue) execute(job *Job) {
	q.mu.Lock()
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
		added, removed, syncErr := q.syncSvc.SyncLibrary(lib)
		if syncErr != nil {
			q.finishJob(job, "", syncErr)
			return
		}
		msg := fmt.Sprintf("added %d, removed %d", added, removed)
		q.finishJob(job, msg, nil)

		// Auto-enqueue matching after sync
		if q.matchSvc != nil {
			if _, enqErr := q.Enqueue(JobTypeMatchLibrary, lib.ID, lib.Name); enqErr != nil {
				slog.Debug("skipping auto-match enqueue", "reason", enqErr)
			}
		}

	case JobTypeMatchLibrary:
		progressFn := func(current, total int) {
			q.mu.Lock()
			job.Progress = &JobProgress{
				Current: current,
				Total:   total,
				Message: fmt.Sprintf("matching %d/%d", current, total),
			}
			q.mu.Unlock()
		}
		matchErr := q.matchSvc.MatchLibrary(lib, progressFn)
		if matchErr != nil {
			q.finishJob(job, "", matchErr)
			return
		}
		q.finishJob(job, "matching complete", nil)
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

	// Move to recent, delete from active map
	delete(q.jobs, job.ID)
	q.recent = append([]*Job{job}, q.recent...)
	if len(q.recent) > 20 {
		q.recent = q.recent[:20]
	}
}
