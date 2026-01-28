package scheduler

import (
	"time"

	"srm-academia-scraper/jobs"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/storage"
)

// AttendanceScheduler periodically enqueue attendance jobs for every user.
type AttendanceScheduler struct {
	db         *storage.SupabaseClient
	jobManager *jobs.Manager
	ticker     *time.Ticker
}

// NewAttendanceScheduler creates a new scheduler that fires every two hours.
func NewAttendanceScheduler(jobManager *jobs.Manager, db *storage.SupabaseClient) *AttendanceScheduler {
	return &AttendanceScheduler{
		db:         db,
		jobManager: jobManager,
		ticker:     time.NewTicker(2 * time.Hour),
	}
}

// Start launches the cron loop.
func (s *AttendanceScheduler) Start() {
	logger.Info("attendance_scheduler", "Starting attendance scheduler", nil)
	go func() {
		// Immediately schedule a run to avoid waiting two hours on cold start.
		s.enqueueForAllUsers()
		for range s.ticker.C {
			s.enqueueForAllUsers()
		}
	}()
}

func (s *AttendanceScheduler) enqueueForAllUsers() {
	logger.Info("attendance_scheduler", "Enqueueing attendance jobs for all users", nil)
	users, err := s.db.ListUsers()
	if err != nil {
		logger.Error("attendance_scheduler", "Failed to list users", err, nil)
		return
	}

	for _, user := range users {
		req := models.JobCreateRequest{
			UserID:   user.ID,
			JobType:  "fetch",
			DataType: "attendance",
			Priority: models.JobPriorityLowest,
		}

		err := s.jobManager.EnqueueJob(req)
		if err != nil {
			if err.Error() == "job already exists" {
				logger.Info("attendance_scheduler", "Skipping duplicate attendance job", map[string]interface{}{
					"user_id":  user.ID,
					"dataType": "attendance",
				})
				continue
			}
			logger.Warn("attendance_scheduler", "Attendance job enqueue failed", map[string]interface{}{
				"user_id": user.ID,
				"error":   err.Error(),
			})
			continue
		}

		logger.Info("attendance_scheduler", "Scheduled attendance job", map[string]interface{}{
			"user_id":  user.ID,
			"dataType": "attendance",
		})
	}
}
