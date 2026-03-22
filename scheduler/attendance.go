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
	db          *storage.SupabaseClient
	ticker      *time.Ticker
	window      time.Duration
	batchSize   int
	nextUserIdx int
}

// NewAttendanceScheduler builds the cron loop. intervalMinutes defaults to 60 if invalid.
// batchSize 0 means enqueue for every user each tick; otherwise rotate a slice of that size.
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
		"interval":              s.window.String(),
		"batch_size_config":     s.batchSize,
		"batch_size_meaning":    "0 = process every user each tick; else rotate slices of this size",
		"idempotency_time_rule": "skip user if fetch/attendance job row has created_at > (now - interval)",
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
	since := now.Add(-s.window)

	logger.Info("attendance_cron", "STEP tick_begin: new cron tick started", map[string]interface{}{
		"tick_id":                    tickID,
		"utc_now":                    now.Format(time.RFC3339),
		"idempotency_cutoff_utc":     since.Format(time.RFC3339),
		"idempotency_rule":           "if jobs.created_at > idempotency_cutoff_utc for fetch+attendance, user is skipped",
		"rotation_next_user_index":   s.nextUserIdx,
		"interval_duration":          s.window.String(),
		"env_batch_size":             s.batchSize,
	})

	// --- Step: load all users from public.users ---
	logger.Info("attendance_cron", "STEP list_users: calling storage.ListUsers (paginated read of public.users)", map[string]interface{}{
		"tick_id": tickID,
	})

	users, err := s.db.ListUsers()
	if err != nil {
		logger.Error("attendance_cron", "STEP list_users FAILED: could not load users; aborting entire tick (no jobs enqueued this tick)", err, map[string]interface{}{
			"tick_id": tickID,
			"hint":    "check Supabase connectivity, RLS, and service key permissions on public.users",
		})
		return
	}

	n := len(users)
	logger.Info("attendance_cron", "STEP list_users OK: user list loaded", map[string]interface{}{
		"tick_id":     tickID,
		"user_count":  n,
		"next_action": "if user_count is 0, tick ends; else compute batch slice and process each user",
	})

	if n == 0 {
		logger.Info("attendance_cron", "STEP tick_end_early: no rows in public.users — nothing to enqueue", map[string]interface{}{
			"tick_id": tickID,
		})
		return
	}

	batch := n
	if s.batchSize > 0 && s.batchSize < n {
		batch = s.batchSize
	}

	firstIdx := s.nextUserIdx % n
	lastIdx := (s.nextUserIdx + batch - 1) % n
	logger.Info("attendance_cron", "STEP batch_scope: this tick processes a slice of users (rotation)", map[string]interface{}{
		"tick_id":          tickID,
		"total_users":      n,
		"batch_size":       batch,
		"first_index":      firstIdx,
		"last_index":       lastIdx,
		"rotation_note":    "next tick continues from next_user_index after this batch",
		"users_in_batch":   batch,
	})

	enqueued := 0
	skippedRecent := 0
	skippedDupQueue := 0
	failed := 0

	for i := 0; i < batch; i++ {
		idx := (s.nextUserIdx + i) % n
		user := users[idx]
		stepNum := i + 1

		logger.Info("attendance_cron", "STEP user_loop: begin user", map[string]interface{}{
			"tick_id":        tickID,
			"user_step":      fmt.Sprintf("%d/%d", stepNum, batch),
			"user_index":     idx,
			"user_id":        user.ID,
			"email":          user.Email,
			"phase":          "1_check_idempotency_window",
		})

		recent, err := s.db.HasRecentAttendanceFetchJob(user.ID, since)
		if err != nil {
			failed++
			logger.Error("attendance_cron", "STEP user_loop ERROR: HasRecentAttendanceFetchJob failed (database query error)", err, map[string]interface{}{
				"tick_id":    tickID,
				"user_step":  fmt.Sprintf("%d/%d", stepNum, batch),
				"user_id":    user.ID,
				"email":      user.Email,
				"hint":       "check public.jobs access, network, and PostgREST filters (user_id, job_type=fetch, data_type=attendance, created_at gt)",
				"since_utc":  since.Format(time.RFC3339),
				"outcome":    "user skipped for this tick; increment failed counter",
			})
			continue
		}
		if recent {
			skippedRecent++
			logger.Info("attendance_cron", "STEP user_loop SKIP: idempotency — recent fetch/attendance job already exists", map[string]interface{}{
				"tick_id":                tickID,
				"user_step":              fmt.Sprintf("%d/%d", stepNum, batch),
				"user_id":                user.ID,
				"email":                  user.Email,
				"reason":                 "created_at > idempotency_cutoff (see tick_begin)",
				"idempotency_cutoff_utc": since.Format(time.RFC3339),
				"outcome":                "no new row inserted in public.jobs for this user",
			})
			continue
		}

		logger.Info("attendance_cron", "STEP user_loop: idempotency OK — proceeding to insert job", map[string]interface{}{
			"tick_id":   tickID,
			"user_step": fmt.Sprintf("%d/%d", stepNum, batch),
			"user_id":   user.ID,
			"email":     user.Email,
			"phase":     "2_enqueue_job_row",
		})

		// Same shape as handlers.handleAndEnqueueDataRequest when token is valid (fetch path).
		req := models.JobCreateRequest{
			UserID:   user.ID,
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
					"user_step": fmt.Sprintf("%d/%d", stepNum, batch),
					"user_id":   user.ID,
					"email":     user.Email,
					"reason":    "storage.EnqueueJob found existing row with status pending or running for same user_id+job_type+data_type",
					"outcome":   "no duplicate job row inserted",
				})
				continue
			}
			failed++
			if errors.Is(err, storage.ErrQueueFull) {
				logger.Error("attendance_cron", "STEP user_loop ERROR: EnqueueJob returned ErrQueueFull (unexpected for fetch-only cron path)", err, map[string]interface{}{
					"tick_id":   tickID,
					"user_step": fmt.Sprintf("%d/%d", stepNum, batch),
					"user_id":   user.ID,
					"email":     user.Email,
					"hint":      "login job limit should not apply to fetch jobs; inspect storage.EnqueueJob if this appears",
				})
			} else {
				logger.Error("attendance_cron", "STEP user_loop ERROR: EnqueueJob failed — job row NOT inserted", err, map[string]interface{}{
					"tick_id":    tickID,
					"user_step":  fmt.Sprintf("%d/%d", stepNum, batch),
					"user_id":    user.ID,
					"email":      user.Email,
					"job_type":   req.JobType,
					"data_type":  req.DataType,
					"priority":   req.Priority,
					"hint":       "check Supabase insert into public.jobs, constraints, and RLS policies",
				})
			}
			continue
		}

		if job == nil {
			failed++
			logger.Error("attendance_cron", "STEP user_loop ERROR: EnqueueJob returned nil job with nil error (unexpected state)", nil, map[string]interface{}{
				"tick_id":   tickID,
				"user_step": fmt.Sprintf("%d/%d", stepNum, batch),
				"user_id":   user.ID,
				"email":     user.Email,
				"hint":      "inspect storage.EnqueueJob return paths for fetch jobs",
			})
			continue
		}

		enqueued++
		logger.Info("attendance_cron", "STEP user_loop OK: JOB INSERTED into public.jobs for attendance fetch", map[string]interface{}{
			"tick_id":    tickID,
			"user_step":  fmt.Sprintf("%d/%d", stepNum, batch),
			"user_id":    user.ID,
			"email":      user.Email,
			"job_id":     job.ID,
			"job_type":   job.JobType,
			"data_type":  job.DataType,
			"status":     job.Status,
			"priority":   job.Priority,
			"outcome":    "worker will pick up this job from the queue",
		})
	}

	s.nextUserIdx = (s.nextUserIdx + batch) % n

	logger.Info("attendance_cron", "STEP tick_summary: cron tick complete", map[string]interface{}{
		"tick_id":             tickID,
		"jobs_inserted":       enqueued,
		"skipped_idempotency": skippedRecent,
		"skipped_dup_queue":   skippedDupQueue,
		"failed":              failed,
		"next_user_index":     s.nextUserIdx,
		"note":                "jobs_inserted = new rows added; skipped_* = intentional; failed = errors during this tick",
	})
}
