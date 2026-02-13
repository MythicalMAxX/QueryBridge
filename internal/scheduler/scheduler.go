// Package scheduler provides scheduled query execution.
package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// Job represents a scheduled query job.
type Job struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Database    string                 `json:"database"`
	Query       map[string]interface{} `json:"query"`
	Schedule    string                 `json:"schedule"` // cron or interval
	Interval    time.Duration          `json:"interval,omitempty"`
	Export      *ExportConfig          `json:"export,omitempty"`
	Enabled     bool                   `json:"enabled"`
	CreatedAt   time.Time              `json:"created_at"`
	LastRun     *time.Time             `json:"last_run,omitempty"`
	NextRun     *time.Time             `json:"next_run,omitempty"`
	RunCount    int                    `json:"run_count"`
	LastError   string                 `json:"last_error,omitempty"`
}

// ExportConfig specifies how to export scheduled query results.
type ExportConfig struct {
	Format   string `json:"format"`   // csv, jsonl, json
	Path     string `json:"path"`     // output directory
	Filename string `json:"filename"` // file prefix
}

// QueryExecutor is a function that executes a query.
type QueryExecutor func(ctx context.Context, database string, query map[string]interface{}) (interface{}, error)

// Scheduler manages scheduled query jobs.
type Scheduler struct {
	jobs     map[string]*Job
	timers   map[string]*time.Timer
	executor QueryExecutor
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
}

// New creates a new scheduler.
func New(executor QueryExecutor) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		jobs:     make(map[string]*Job),
		timers:   make(map[string]*time.Timer),
		executor: executor,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// AddJob adds a new scheduled job.
func (s *Scheduler) AddJob(job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job.ID == "" {
		job.ID = generateJobID(job.Name)
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}

	// Parse interval from schedule string if needed
	if job.Interval == 0 && job.Schedule != "" {
		interval, err := parseSchedule(job.Schedule)
		if err != nil {
			return fmt.Errorf("invalid schedule: %w", err)
		}
		job.Interval = interval
	}

	if job.Interval <= 0 {
		return fmt.Errorf("interval must be positive")
	}

	job.Enabled = true
	s.jobs[job.ID] = job

	// Schedule first run
	s.scheduleJob(job)

	log.Printf("[Scheduler] Added job %s (%s), interval: %v", job.ID, job.Name, job.Interval)
	return nil
}

// RemoveJob removes a scheduled job.
func (s *Scheduler) RemoveJob(idOrName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job := s.findJob(idOrName)
	if job == nil {
		return fmt.Errorf("job not found: %s", idOrName)
	}

	// Cancel timer
	if timer, ok := s.timers[job.ID]; ok {
		timer.Stop()
		delete(s.timers, job.ID)
	}

	delete(s.jobs, job.ID)
	log.Printf("[Scheduler] Removed job %s", job.ID)
	return nil
}

// PauseJob pauses a scheduled job.
func (s *Scheduler) PauseJob(idOrName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job := s.findJob(idOrName)
	if job == nil {
		return fmt.Errorf("job not found: %s", idOrName)
	}

	job.Enabled = false
	if timer, ok := s.timers[job.ID]; ok {
		timer.Stop()
	}

	log.Printf("[Scheduler] Paused job %s", job.ID)
	return nil
}

// ResumeJob resumes a paused job.
func (s *Scheduler) ResumeJob(idOrName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job := s.findJob(idOrName)
	if job == nil {
		return fmt.Errorf("job not found: %s", idOrName)
	}

	job.Enabled = true
	s.scheduleJob(job)

	log.Printf("[Scheduler] Resumed job %s", job.ID)
	return nil
}

// ListJobs returns all jobs, optionally filtered by database.
func (s *Scheduler) ListJobs(database string) []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Job
	for _, job := range s.jobs {
		if database == "" || job.Database == database {
			result = append(result, job)
		}
	}
	return result
}

// GetJob retrieves a job by ID or name.
func (s *Scheduler) GetJob(idOrName string) *Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.findJob(idOrName)
}

// findJob finds a job by ID or name (must hold lock).
func (s *Scheduler) findJob(idOrName string) *Job {
	if job, ok := s.jobs[idOrName]; ok {
		return job
	}
	for _, job := range s.jobs {
		if job.Name == idOrName {
			return job
		}
	}
	return nil
}

// scheduleJob schedules the next run for a job (must hold lock).
func (s *Scheduler) scheduleJob(job *Job) {
	if !job.Enabled {
		return
	}

	// Cancel existing timer
	if timer, ok := s.timers[job.ID]; ok {
		timer.Stop()
	}

	nextRun := time.Now().Add(job.Interval)
	job.NextRun = &nextRun

	timer := time.AfterFunc(job.Interval, func() {
		s.runJob(job.ID)
	})
	s.timers[job.ID] = timer
}

// runJob executes a scheduled job.
func (s *Scheduler) runJob(jobID string) {
	s.mu.Lock()
	job, ok := s.jobs[jobID]
	if !ok || !job.Enabled {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	log.Printf("[Scheduler] Running job %s (%s)", job.ID, job.Name)

	// Execute query
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()

	_, err := s.executor(ctx, job.Database, job.Query)

	s.mu.Lock()
	now := time.Now()
	job.LastRun = &now
	job.RunCount++
	if err != nil {
		job.LastError = err.Error()
		log.Printf("[Scheduler] Job %s failed: %v", job.ID, err)
	} else {
		job.LastError = ""
		log.Printf("[Scheduler] Job %s completed successfully", job.ID)
	}

	// Schedule next run
	s.scheduleJob(job)
	s.mu.Unlock()
}

// Stop stops all scheduled jobs and the scheduler.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, timer := range s.timers {
		timer.Stop()
	}
	s.timers = make(map[string]*time.Timer)
	s.cancel()

	log.Printf("[Scheduler] Stopped")
}

// parseSchedule converts a schedule string to an interval.
// Supports: "1h", "30m", "5m", "24h", etc.
func parseSchedule(schedule string) (time.Duration, error) {
	// Try parsing as duration directly
	if d, err := time.ParseDuration(schedule); err == nil {
		return d, nil
	}

	// Common aliases
	switch schedule {
	case "hourly":
		return time.Hour, nil
	case "daily":
		return 24 * time.Hour, nil
	case "weekly":
		return 7 * 24 * time.Hour, nil
	case "every_minute":
		return time.Minute, nil
	case "every_5_minutes":
		return 5 * time.Minute, nil
	default:
		return 0, fmt.Errorf("unknown schedule format: %s", schedule)
	}
}

// generateJobID creates a unique job ID.
func generateJobID(name string) string {
	return fmt.Sprintf("%s_%d", name, time.Now().UnixNano()%10000)
}
