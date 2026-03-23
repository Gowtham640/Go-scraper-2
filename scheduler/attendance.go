package scheduler

import (
	"errors"
	"fmt"
	"time"

	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/storage"
)

// AttendanceScheduler enqueues fetch/attendance jobs on a fixed interval; workers claim and run them.
type AttendanceScheduler struct {
	db        *storage.SupabaseClient
	ticker    *time.Ticker
	window    time.Duration
	batchSize int
}

// NewAttendanceScheduler builds the cron loop. intervalMinutes defaults to 60 if invalid.
func NewAttendanceScheduler(db *storage.SupabaseClient, intervalMinutes, batchSize int) *AttendanceScheduler {
	if intervalMinutes < 1 {
		intervalMinutes = 60
	}
	w := time.Duration(intervalMinutes) * time.Minute
	return &AttendanceScheduler{
		db:        db,
		ticker:    time.NewTicker(w),
		window:    w,
		batchSize: batchSize,
	}
}

// Start runs the first enqueue immediately, then on each tick. Only inserts jobs; workers process them.
func (s *AttendanceScheduler) Start() {
	logger.Info("attendance_cron", "STEP scheduler_start: attendance cron goroutine will run (enqueue only, workers execute jobs)", map[string]interface{}{
		"interval":           s.window.String(),
		"batch_size_config":  s.batchSize,
		"selection_source":   "public.user_cache",
		"selection_rule":     "data_type=attendance AND expires_at < now",
		"batch_size_meaning": "enqueue up to CRON_BATCH_SIZE users each tick",
	})
	go func() {
		logger.Info("attendance_cron", "STEP scheduler_goroutine: entered; running first enqueue tick immediately", nil)
		s.enqueueBatch()
		logger.Info("attendance_cron", "STEP scheduler_goroutine: entering ticker loop (subsequent ticks on interval)", nil)
		for range s.ticker.C {
			s.enqueueBatch()
		}
	}()
}

func (s *AttendanceScheduler) enqueueBatch() {
	tickID := fmt.Sprintf("%d", time.Now().UnixNano())
	now := time.Now().UTC()

	logger.Info("attendance_cron", "STEP tick_begin: new cron tick started", map[string]interface{}{
		"tick_id":           tickID,
		"utc_now":           now.Format(time.RFC3339),
		"interval_duration": s.window.String(),
		"env_batch_size":    s.batchSize,
	})

	if s.batchSize <= 0 {
		logger.Warn("attendance_cron", "STEP tick_end_early: CRON_BATCH_SIZE must be > 0 for attendance cache selection", map[string]interface{}{
			"tick_id":    tickID,
			"batch_size": s.batchSize,
		})
		return
	}

	logger.Info("attendance_cron", "STEP select_users: querying public.user_cache for expired attendance rows", map[string]interface{}{
		"tick_id":    tickID,
		"batch_size": s.batchSize,
	})

	userIDs, err := s.db.ListUsersWithExpiredAttendanceCache(s.batchSize)
	if err != nil {
		logger.Error("attendance_cron", "STEP select_users FAILED: could not load expired attendance users", err, map[string]interface{}{
			"tick_id":    tickID,
			"batch_size": s.batchSize,
		})
		return
	}

	if len(userIDs) == 0 {
		logger.Info("attendance_cron", "STEP tick_end_early: no expired attendance cache rows", map[string]interface{}{
			"tick_id": tickID,
		})
		return
	}

	logger.Info("attendance_cron", "STEP select_users OK: expired attendance users resolved", map[string]interface{}{
		"tick_id":        tickID,
		"selected_count": len(userIDs),
	})

	enqueued := 0
	skippedDupQueue := 0
	failed := 0

	for i, userID := range userIDs {
		stepNum := i + 1

		req := models.JobCreateRequest{
			UserID:   userID,
			JobType:  "fetch",
			DataType: models.AttendanceDataType,
			Priority: 10,
		}

		job, _, err := s.db.EnqueueJob(req)
		if err != nil {
			if err.Error() == "job already exists" {
				skippedDupQueue++
				logger.Info("attendance_cron", "STEP user_loop SKIP: queue dedup — pending or running job already exists", map[string]interface{}{
					"tick_id":   tickID,
					"user_step": fmt.Sprintf("%d/%d", stepNum, len(userIDs)),
					"user_id":   userID,
					"reason":    "storage.EnqueueJob found existing row with status pending or running for same user_id+job_type+data_type",
					"outcome":   "no duplicate job row inserted",
				})
				continue
			}
			failed++
			if errors.Is(err, storage.ErrQueueFull) {
				logger.Error("attendance_cron", "STEP user_loop ERROR: EnqueueJob returned ErrQueueFull (unexpected for fetch-only cron path)", err, map[string]interface{}{
					"tick_id":   tickID,
					"user_step": fmt.Sprintf("%d/%d", stepNum, len(userIDs)),
					"user_id":   userID,
					"hint":      "login job limit should not apply to fetch jobs; inspect storage.EnqueueJob if this appears",
				})
			} else {
				logger.Error("attendance_cron", "STEP user_loop ERROR: EnqueueJob failed — job row NOT inserted", err, map[string]interface{}{
					"tick_id":   tickID,
					"user_step": fmt.Sprintf("%d/%d", stepNum, len(userIDs)),
					"user_id":   userID,
					"job_type":  req.JobType,
					"data_type": req.DataType,
					"priority":  req.Priority,
					"hint":      "check Supabase insert into public.jobs, constraints, and RLS policies",
				})
			}
			continue
		}

		if job == nil {
			failed++
			logger.Error("attendance_cron", "STEP user_loop ERROR: EnqueueJob returned nil job with nil error (unexpected state)", nil, map[string]interface{}{
				"tick_id":   tickID,
				"user_step": fmt.Sprintf("%d/%d", stepNum, len(userIDs)),
				"user_id":   userID,
				"hint":      "inspect storage.EnqueueJob return paths for fetch jobs",
			})
			continue
		}

		enqueued++
		logger.Info("attendance_cron", "STEP user_loop OK: JOB INSERTED into public.jobs for attendance fetch", map[string]interface{}{
			"tick_id":   tickID,
			"user_step": fmt.Sprintf("%d/%d", stepNum, len(userIDs)),
			"user_id":   userID,
			"job_id":    job.ID,
			"job_type":  job.JobType,
			"data_type": job.DataType,
			"status":    job.Status,
			"priority":  job.Priority,
			"outcome":   "worker will pick up this job from the queue",
		})
	}

	logger.Info("attendance_cron", "STEP tick_summary: cron tick complete", map[string]interface{}{
		"tick_id":           tickID,
		"selected_users":    len(userIDs),
		"jobs_inserted":     enqueued,
		"skipped_dup_queue": skippedDupQueue,
		"failed":            failed,
		"note":              "selection is public.user_cache where attendance expires_at is in the past",
	})
}
