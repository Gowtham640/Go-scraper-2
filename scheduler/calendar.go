package scheduler

import (
	"fmt"
	"time"

	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/storage"
)

const (
	calendarCronTargetEmail = "gr8790@srmist.edu.in"
	calendarCronUserType    = "old"
)

// CalendarScheduler enqueues one calendar fetch job every 24 hours for a single allowed user.
type CalendarScheduler struct {
	db     *storage.SupabaseClient
	ticker *time.Ticker
	window time.Duration
}

// NewCalendarScheduler builds the daily calendar cron loop.
func NewCalendarScheduler(db *storage.SupabaseClient) *CalendarScheduler {
	window := 24 * time.Hour
	return &CalendarScheduler{
		db:     db,
		ticker: time.NewTicker(window),
		window: window,
	}
}

// Start runs one enqueue attempt immediately, then every 24 hours.
func (s *CalendarScheduler) Start() {
	logger.Info("calendar_cron", "STEP scheduler_start: calendar cron started", map[string]interface{}{
		"interval":          s.window.String(),
		"target_email":      calendarCronTargetEmail,
		"required_userType": calendarCronUserType,
	})

	go func() {
		s.enqueueCalendarJob()
		for range s.ticker.C {
			s.enqueueCalendarJob()
		}
	}()
}

func (s *CalendarScheduler) enqueueCalendarJob() {
	tickID := fmt.Sprintf("%d", time.Now().UnixNano())
	logger.Info("calendar_cron", "STEP tick_begin: resolving allowed calendar cron user", map[string]interface{}{
		"tick_id":           tickID,
		"target_email":      calendarCronTargetEmail,
		"required_userType": calendarCronUserType,
	})

	userID, password, err := s.db.GetUserCredentialsByEmail(calendarCronTargetEmail)
	if err != nil {
		logger.Error("calendar_cron", "STEP resolve_user FAILED: could not load user id/password from public.users", err, map[string]interface{}{
			"tick_id":      tickID,
			"target_email": calendarCronTargetEmail,
		})
		return
	}

	if password == "" {
		logger.Error("calendar_cron", "STEP resolve_user FAILED: decrypted password empty", nil, map[string]interface{}{
			"tick_id":      tickID,
			"target_email": calendarCronTargetEmail,
			"user_id":      userID,
		})
		return
	}

	req := models.JobCreateRequest{
		UserID:   userID,
		JobType:  "fetch",
		DataType: "calendar",
		Priority: 10,
	}

	job, _, err := s.db.EnqueueJob(req)
	if err != nil {
		if err.Error() == "job already exists" {
			logger.Info("calendar_cron", "STEP enqueue_job SKIP: dedup found pending/running calendar job", map[string]interface{}{
				"tick_id": tickID,
				"user_id": userID,
				"email":   calendarCronTargetEmail,
			})
			return
		}
		logger.Error("calendar_cron", "STEP enqueue_job FAILED: could not insert calendar fetch job", err, map[string]interface{}{
			"tick_id": tickID,
			"user_id": userID,
			"email":   calendarCronTargetEmail,
		})
		return
	}

	if job == nil {
		logger.Error("calendar_cron", "STEP enqueue_job FAILED: nil job returned", nil, map[string]interface{}{
			"tick_id": tickID,
			"user_id": userID,
			"email":   calendarCronTargetEmail,
		})
		return
	}

	logger.Info("calendar_cron", "STEP enqueue_job OK: calendar job inserted into public.jobs", map[string]interface{}{
		"tick_id":   tickID,
		"job_id":    job.ID,
		"user_id":   job.UserID,
		"job_type":  job.JobType,
		"data_type": job.DataType,
	})
}
