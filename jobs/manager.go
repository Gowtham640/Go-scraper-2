package jobs

import (
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/storage"
	"srm-academia-scraper/worker"
	"time"
)

// Manager coordinates job creation between handlers and worker
type Manager struct {
	db     *storage.SupabaseClient
	worker *worker.Worker
}

// NewManager creates a new job manager
func NewManager(db *storage.SupabaseClient, worker *worker.Worker) *Manager {
	return &Manager{
		db:     db,
		worker: worker,
	}
}

// GetUserCacheWithTimestamp retrieves cached data with timestamp
func (m *Manager) GetUserCacheWithTimestamp(userID, dataType string) (interface{}, *time.Time, *time.Time, error) {
	return m.db.GetUserCacheWithTimestamp(userID, dataType)
}

// GetToken retrieves token data
func (m *Manager) GetToken(userID string) (*models.TokenData, error) {
	return m.db.GetToken(userID)
}

// SaveUserEncryptedPassword encrypts and upserts the portal password to public.users (same as storage).
func (m *Manager) SaveUserEncryptedPassword(userID, email, password string) error {
	return m.db.SaveUserEncryptedPassword(userID, email, password)
}

// GetUserCache retrieves cached data for a user and data type
func (m *Manager) GetUserCache(userID, dataType string) (interface{}, error) {
	return m.db.GetUserCache(userID, dataType)
}

// EnqueueJob handles job creation and coordinates with worker for login jobs
func (m *Manager) EnqueueJob(req models.JobCreateRequest) error {
	logger.Info("job_manager_enqueue", "Enqueueing job", map[string]interface{}{
		"user_id":   req.UserID,
		"job_type":  req.JobType,
		"data_type": req.DataType,
	})

	// Use the database method that returns appropriate objects
	job, loginReq, err := m.db.EnqueueJob(req)
	if err != nil {
		if err.Error() == "queue_full" {
			return err
		}
		if err.Error() == "job already exists" {
			return nil // This is not an error for handlers
		}
		return err
	}

	// Handle login requests by sending to worker
	if loginReq != nil {
		workerLoginReq := models.WorkerLoginRequest{
			UserID:             loginReq.UserID,
			Email:              loginReq.Email,
			Password:           loginReq.Password,
			Priority:           loginReq.Priority,
			RequestedDataTypes: loginReq.RequestedDataTypes,
		}
		m.worker.EnqueueLoginRequest(workerLoginReq)
		return nil
	}

	// Handle regular jobs (fetch jobs)
	if job != nil {
		return nil
	}

	return nil
}
