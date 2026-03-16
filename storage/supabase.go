package storage

import (
	"encoding/json"
	"fmt"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"time"

	"github.com/google/uuid"
	"github.com/supabase-community/gotrue-go/types"
	"github.com/supabase-community/postgrest-go"
	supabase "github.com/supabase-community/supabase-go"
)

type SupabaseClient struct {
	client *supabase.Client
}

func NewSupabaseClient(url, key string) (*SupabaseClient, error) {
	logger.Info("supabase_init", "Initializing Supabase client", nil)

	client, err := supabase.NewClient(url, key, nil)
	if err != nil {
		logger.Error("supabase_init", "Failed to create Supabase client", err, nil)
		return nil, err
	}

	logger.Info("supabase_init", "Supabase client initialized successfully", nil)
	return &SupabaseClient{client: client}, nil
}

// CreateAuthUser creates a user in auth.users table
func (s *SupabaseClient) CreateAuthUser(email, password string) (string, error) {
	logger.InfoWithUser(email, "create_auth_user", "Creating auth user", nil)

	// Use Supabase Admin API to create user
	user, err := s.client.Auth.Signup(types.SignupRequest{
		Email:    email,
		Password: password,
	})
	if err != nil {
		logger.ErrorWithUser(email, "create_auth_user", "Failed to create auth user", err, nil)
		return "", err
	}

	logger.InfoWithUser(email, "create_auth_user", "Auth user created successfully", map[string]interface{}{
		"user_id": user.ID,
	})
	return user.ID.String(), nil
}

// UpsertUser upserts user data in public.users table
func (s *SupabaseClient) UpsertUser(userID, email string, userInfo *models.UserInfo) error {
	logger.InfoWithUser(email, "upsert_user", "Upserting user data", map[string]interface{}{
		"batch": userInfo.Batch,
	})

	data := map[string]interface{}{
		"id":             userID,
		"email":          email,
		"role":           "public",
		"semester":       userInfo.Semester,
		"name":           userInfo.Name,
		"regnumber":      userInfo.RegNumber,
		"department":     userInfo.Department,
		"mobile":         userInfo.Mobile,
		"program":        userInfo.Program,
		"batch":          userInfo.Batch,
		"year":           userInfo.Year,
		"section":        userInfo.Section,
		"specialization": userInfo.Specialization,
	}

	var result []map[string]interface{}
	var err error
	_, err = s.client.From("users").Upsert(data, "", "", "").ExecuteTo(&result)
	if err != nil {
		logger.ErrorWithUser(email, "upsert_user", "Failed to upsert user data", err, nil)
		return err
	}

	logger.InfoWithUser(email, "upsert_user", "User data upserted successfully", nil)
	return nil
}

// UpsertToken stores or updates session tokens in public.tokens table
func (s *SupabaseClient) UpsertToken(userID, email, tokens string, expiryDays int) error {
	logger.InfoWithUser(email, "upsert_token", "Upserting token", map[string]interface{}{
		"expiry_days": expiryDays,
	})

	expiryTimestamp := time.Now().Add(time.Duration(expiryDays) * 24 * time.Hour)

	data := map[string]interface{}{
		"user_id":          userID,
		"tokens":           tokens,
		"expiry_timestamp": expiryTimestamp.Format(time.RFC3339),
		"email":            email,
	}

	var result []map[string]interface{}
	var err error

	var existing []map[string]interface{}
	_, err = s.client.From("tokens").
		Select("id", "", false).
		Eq("user_id", userID).
		ExecuteTo(&existing)
	if err != nil {
		logger.ErrorWithUser(email, "upsert_token", "Failed to query existing token", err, nil)
		return err
	}

	if len(existing) > 0 {
		updateData := map[string]interface{}{
			"tokens":           tokens,
			"expiry_timestamp": expiryTimestamp.Format(time.RFC3339),
			"email":            email,
		}

		logger.InfoWithUser(email, "upsert_token", "Updating existing token", map[string]interface{}{"token_id": existing[0]["id"]})
		_, err = s.client.From("tokens").
			Update(updateData, "", "").
			Eq("user_id", userID).
			ExecuteTo(&result)
	} else {
		logger.InfoWithUser(email, "upsert_token", "Inserting new token", nil)
		_, err = s.client.From("tokens").
			Insert(data, false, "", "", "").
			ExecuteTo(&result)
	}

	if err != nil {
		logger.ErrorWithUser(email, "upsert_token", "Failed to upsert token", err, nil)
		return err
	}

	logger.InfoWithUser(email, "upsert_token", "Token upserted successfully", nil)
	return nil
}

// GetToken retrieves token by user ID
func (s *SupabaseClient) GetToken(userID string) (*models.TokenData, error) {
	logger.Info("get_token", "Fetching token from database", map[string]interface{}{
		"user_id": userID,
	})

	var result []map[string]interface{}
	var err error
	_, err = s.client.From("tokens").
		Select("*", "", false).
		Eq("user_id", userID).
		ExecuteTo(&result)

	if err != nil {
		logger.Error("get_token", "Failed to fetch token", err, nil)
		return nil, err
	}

	if len(result) == 0 {
		logger.Warn("get_token", "No token found for user", map[string]interface{}{
			"user_id": userID,
		})
		return nil, fmt.Errorf("no token found for user")
	}

	// Parse the result
	tokenData := &models.TokenData{
		UserID: result[0]["user_id"].(string),
		Tokens: result[0]["tokens"].(string),
		Email:  result[0]["email"].(string),
	}

	// Parse expiry timestamp
	if expiryStr, ok := result[0]["expiry_timestamp"].(string); ok {
		expiry, err := time.Parse(time.RFC3339, expiryStr)
		if err == nil {
			tokenData.ExpiryTimestamp = expiry
		}
	}

	logger.Info("get_token", "Token fetched successfully", nil)
	return tokenData, nil
}

// UpsertUserCache stores data in public.user_cache table
func (s *SupabaseClient) UpsertUserCache(userID, dataType string, data interface{}, expiresInHours int) error {
	logger.Info("upsert_cache", "Upserting user cache", map[string]interface{}{
		"user_id":   userID,
		"data_type": dataType,
	})

	expiresAt := time.Now().Add(time.Duration(expiresInHours) * time.Hour)

	// Check if record exists
	var existingResult []map[string]interface{}
	_, err := s.client.From("user_cache").
		Select("id", "", false).
		Eq("user_id", userID).
		Eq("data_type", dataType).
		ExecuteTo(&existingResult)

	if err != nil {
		logger.Error("upsert_cache", "Failed to check existing cache", err, nil)
		return err
	}

	var result []map[string]interface{}
	if len(existingResult) > 0 {
		// Update existing record
		updateData := map[string]interface{}{
			"data":       data,
			"updated_at": time.Now().Format(time.RFC3339),
			"expires_at": expiresAt.Format(time.RFC3339),
		}

		_, err = s.client.From("user_cache").
			Update(updateData, "", "").
			Eq("user_id", userID).
			Eq("data_type", dataType).
			ExecuteTo(&result)
	} else {
		// Insert new record
		insertData := map[string]interface{}{
			"user_id":    userID,
			"data_type":  dataType,
			"data":       data,
			"updated_at": time.Now().Format(time.RFC3339),
			"expires_at": expiresAt.Format(time.RFC3339),
		}

		_, err = s.client.From("user_cache").Insert(insertData, false, "", "", "").ExecuteTo(&result)
	}

	if err != nil {
		logger.Error("upsert_cache", "Failed to upsert cache", err, nil)
		return err
	}

	logger.Info("upsert_cache", "Cache upserted successfully", nil)
	return nil
}

// UpsertCalendar stores calendar data in public.calendar table
func (s *SupabaseClient) UpsertCalendar(course string, semester int, data interface{}) error {
	logger.Info("upsert_calendar", "Upserting calendar", map[string]interface{}{
		"course":   course,
		"semester": semester,
	})

	// Check if record exists
	var existingResult []map[string]interface{}
	_, err := s.client.From("calendar").
		Select("id", "", false).
		Eq("course", course).
		Eq("semester", fmt.Sprintf("%d", semester)).
		ExecuteTo(&existingResult)

	if err != nil {
		logger.Error("upsert_calendar", "Failed to check existing calendar", err, nil)
		return err
	}

	var result []map[string]interface{}
	if len(existingResult) > 0 {
		// Update existing record
		updateData := map[string]interface{}{
			"data":       data,
			"updated_at": time.Now().Format(time.RFC3339),
		}

		_, err = s.client.From("calendar").
			Update(updateData, "", "").
			Eq("course", course).
			Eq("semester", fmt.Sprintf("%d", semester)).
			ExecuteTo(&result)
	} else {
		// Insert new record
		insertData := map[string]interface{}{
			"id":         uuid.New().String(),
			"course":     course,
			"semester":   semester,
			"data":       data,
			"updated_at": time.Now().Format(time.RFC3339),
		}

		_, err = s.client.From("calendar").Insert(insertData, false, "", "", "").ExecuteTo(&result)
	}

	if err != nil {
		logger.Error("upsert_calendar", "Failed to upsert calendar", err, nil)
		return err
	}

	logger.Info("upsert_calendar", "Calendar upserted successfully", nil)
	return nil
}

// GetUserByEmail retrieves user by email
func (s *SupabaseClient) GetUserByEmail(email string) (string, error) {
	logger.Info("get_user_by_email", "Fetching user by email", map[string]interface{}{
		"email": email,
	})

	var result []map[string]interface{}
	var err error
	_, err = s.client.From("users").
		Select("id", "", false).
		Eq("email", email).
		ExecuteTo(&result)

	if err != nil {
		logger.Error("get_user_by_email", "Failed to fetch user", err, nil)
		return "", err
	}

	if len(result) == 0 {
		logger.Warn("get_user_by_email", "User not found", map[string]interface{}{
			"email": email,
		})
		return "", fmt.Errorf("user not found")
	}

	userID := result[0]["id"].(string)
	logger.Info("get_user_by_email", "User found", map[string]interface{}{
		"user_id": userID,
	})
	return userID, nil
}

// GetUserBatch retrieves user's batch from public.users table
func (s *SupabaseClient) GetUserBatch(userID string) (string, error) {
	logger.Info("get_user_batch", "Fetching user batch", map[string]interface{}{
		"user_id": userID,
	})

	var result []map[string]interface{}
	var err error
	_, err = s.client.From("users").
		Select("batch", "", false).
		Eq("id", userID).
		ExecuteTo(&result)

	if err != nil {
		logger.Error("get_user_batch", "Failed to fetch user batch", err, nil)
		return "", err
	}

	if len(result) == 0 {
		logger.Warn("get_user_batch", "User not found", map[string]interface{}{
			"user_id": userID,
		})
		return "", fmt.Errorf("user not found")
	}

	batch := result[0]["batch"].(string)
	logger.Info("get_user_batch", "User batch found", map[string]interface{}{
		"user_id":    userID,
		"batch":      batch,
		"batch_type": fmt.Sprintf("%T", result[0]["batch"]),
		"batch_raw":  result[0]["batch"],
	})
	return batch, nil
}

// GetUserCache retrieves cached data for a user by data_type
func (s *SupabaseClient) GetUserCache(userID, dataType string) (interface{}, error) {
	logger.Info("get_user_cache", "Fetching user cache", map[string]interface{}{
		"user_id":   userID,
		"data_type": dataType,
	})

	var result []map[string]interface{}
	var err error
	_, err = s.client.From("user_cache").
		Select("data", "", false).
		Eq("user_id", userID).
		Eq("data_type", dataType).
		ExecuteTo(&result)

	if err != nil {
		logger.Error("get_user_cache", "Failed to fetch user cache", err, nil)
		return nil, err
	}

	if len(result) == 0 {
		logger.Info("get_user_cache", "No cached data found", map[string]interface{}{
			"user_id":   userID,
			"data_type": dataType,
		})
		return nil, fmt.Errorf("no cached data found")
	}

	rawData := result[0]["data"]

	// Unmarshal based on data type
	var unmarshaledData interface{}
	switch dataType {
	case "courses":
		var coursesData models.CoursesData
		if err := s.unmarshalData(rawData, &coursesData); err != nil {
			logger.Error("get_user_cache", "Failed to unmarshal courses data", err, nil)
			return nil, err
		}
		unmarshaledData = coursesData
	case "timetable":
		var timetableData models.TimetableData
		if err := s.unmarshalData(rawData, &timetableData); err != nil {
			logger.Error("get_user_cache", "Failed to unmarshal timetable data", err, nil)
			return nil, err
		}
		unmarshaledData = timetableData
	case "calendar":
		var calendarData models.CalendarData
		if err := s.unmarshalData(rawData, &calendarData); err != nil {
			logger.Error("get_user_cache", "Failed to unmarshal calendar data", err, nil)
			return nil, err
		}
		unmarshaledData = calendarData
	case "user":
		var userInfo models.UserInfo
		if err := s.unmarshalData(rawData, &userInfo); err != nil {
			logger.Error("get_user_cache", "Failed to unmarshal user data", err, nil)
			return nil, err
		}
		unmarshaledData = userInfo
	default:
		logger.Warn("get_user_cache", "Unknown data type, returning raw data", map[string]interface{}{
			"data_type": dataType,
		})
		unmarshaledData = rawData
	}

	logger.Info("get_user_cache", "User cache found and unmarshaled", map[string]interface{}{
		"user_id":   userID,
		"data_type": dataType,
	})
	return unmarshaledData, nil
}

// unmarshalData unmarshals JSON data from Supabase to the target struct
func (s *SupabaseClient) unmarshalData(rawData interface{}, target interface{}) error {
	// Supabase returns JSON data, so we need to marshal it to JSON bytes first, then unmarshal
	jsonBytes, err := json.Marshal(rawData)
	if err != nil {
		return fmt.Errorf("failed to marshal raw data: %v", err)
	}

	err = json.Unmarshal(jsonBytes, target)
	if err != nil {
		return fmt.Errorf("failed to unmarshal to target type: %v", err)
	}

	return nil
}

// GetUserCacheWithTimestamp retrieves cached data with timestamp for freshness checking
func (s *SupabaseClient) GetUserCacheWithTimestamp(userID, dataType string) (interface{}, *time.Time, *time.Time, error) {
	logger.Info("get_user_cache_timestamp", "Fetching user cache with timestamp", map[string]interface{}{
		"user_id":   userID,
		"data_type": dataType,
	})

	var result []map[string]interface{}
	var err error
	_, err = s.client.From("user_cache").
		Select("data, updated_at, expires_at", "", false).
		Eq("user_id", userID).
		Eq("data_type", dataType).
		ExecuteTo(&result)

	if err != nil {
		logger.Error("get_user_cache_timestamp", "Failed to fetch user cache", err, nil)
		return nil, nil, nil, err
	}

	if len(result) == 0 {
		logger.Info("get_user_cache_timestamp", "No cached data found", map[string]interface{}{
			"user_id":   userID,
			"data_type": dataType,
		})
		return nil, nil, nil, fmt.Errorf("no cached data found")
	}

	rawData := result[0]["data"]
	updatedAtStr := result[0]["updated_at"].(string)
	updatedAt, err := time.Parse(time.RFC3339, updatedAtStr)
	if err != nil {
		logger.Warn("get_user_cache_timestamp", "Failed to parse updated_at", map[string]interface{}{"error": err.Error()})
		// Still try to unmarshal data even if timestamp parsing fails
	}

	var expiresAt *time.Time
	if expiresRaw, exists := result[0]["expires_at"]; exists && expiresRaw != nil {
		if expiresStr, ok := expiresRaw.(string); ok && expiresStr != "" {
			if parsedExpires, parseErr := time.Parse(time.RFC3339, expiresStr); parseErr == nil {
				expiresAt = &parsedExpires
			} else {
				logger.Warn("get_user_cache_timestamp", "Failed to parse expires_at", map[string]interface{}{"error": parseErr.Error()})
			}
		}
	}

	// Unmarshal based on data type
	var unmarshaledData interface{}
	switch dataType {
	case "courses":
		var coursesData models.CoursesData
		if err := s.unmarshalData(rawData, &coursesData); err != nil {
			logger.Error("get_user_cache_timestamp", "Failed to unmarshal courses data", err, nil)
			return nil, nil, nil, err
		}
		unmarshaledData = coursesData
	case "timetable":
		var timetableData models.TimetableData
		if err := s.unmarshalData(rawData, &timetableData); err != nil {
			logger.Error("get_user_cache_timestamp", "Failed to unmarshal timetable data", err, nil)
			return nil, nil, nil, err
		}
		unmarshaledData = timetableData
	case "calendar":
		var calendarData models.CalendarData
		if err := s.unmarshalData(rawData, &calendarData); err != nil {
			logger.Error("get_user_cache_timestamp", "Failed to unmarshal calendar data", err, nil)
			return nil, nil, nil, err
		}
		unmarshaledData = calendarData
	case "user":
		var userInfo models.UserInfo
		if err := s.unmarshalData(rawData, &userInfo); err != nil {
			logger.Error("get_user_cache_timestamp", "Failed to unmarshal user data", err, nil)
			return nil, nil, nil, err
		}
		unmarshaledData = userInfo
	default:
		logger.Warn("get_user_cache_timestamp", "Unknown data type, returning raw data", map[string]interface{}{
			"data_type": dataType,
		})
		unmarshaledData = rawData
	}

	logger.Info("get_user_cache_timestamp", "User cache found and unmarshaled", map[string]interface{}{
		"user_id":    userID,
		"data_type":  dataType,
		"updated_at": updatedAt,
		"expires_at": expiresAt,
	})
	return unmarshaledData, &updatedAt, expiresAt, nil
}

// EnqueueJob creates a new job with deduplication logic
// For login jobs, returns a WorkerLoginRequest that should be sent to the worker
func (s *SupabaseClient) EnqueueJob(req models.JobCreateRequest) (*models.Job, *models.WorkerLoginRequest, error) {
	logger.Info("enqueue_job", "Attempting to enqueue job", map[string]interface{}{
		"user_id":   req.UserID,
		"job_type":  req.JobType,
		"data_type": req.DataType,
		"priority":  req.Priority,
	})

	// Special handling for login jobs - don't store in DB, return login request
	if req.JobType == "login" {
		// Check global Playwright limit
		runningCount, err := s.CountRunningLoginJobs()
		if err != nil {
			logger.Error("enqueue_job", "Failed to count running login jobs", err, nil)
			return nil, nil, err
		}

		if runningCount >= 3 {
			logger.Warn("enqueue_job", "Playwright limit reached", map[string]interface{}{
				"running_login_jobs": runningCount,
			})
			return nil, nil, fmt.Errorf("queue_full")
		}

		// Return login request for worker
		loginReq := &models.WorkerLoginRequest{
			UserID:             req.UserID,
			Email:              req.Email,
			Password:           req.Password,
			Priority:           req.Priority,
			RequestedDataTypes: req.RequestedDataTypes,
		}

		logger.Info("enqueue_job", "Login request created", map[string]interface{}{
			"user_id": req.UserID,
			"email":   req.Email,
		})
		return nil, loginReq, nil
	}

	// Regular job handling for fetch jobs
	// Check for existing active job (deduplication)
	var existingResult []map[string]interface{}
	_, err := s.client.From("jobs").
		Select("id, status", "", false).
		Eq("user_id", req.UserID).
		Eq("job_type", req.JobType).
		Eq("data_type", req.DataType).
		In("status", []string{"pending", "running"}).
		ExecuteTo(&existingResult)

	if err != nil {
		logger.Error("enqueue_job", "Failed to check existing jobs", err, nil)
		return nil, nil, err
	}

	if len(existingResult) > 0 {
		logger.Info("enqueue_job", "Job already exists, skipping enqueue", map[string]interface{}{
			"user_id":         req.UserID,
			"job_type":        req.JobType,
			"data_type":       req.DataType,
			"existing_status": existingResult[0]["status"],
		})
		return nil, nil, fmt.Errorf("job already exists")
	}

	// Insert new job
	jobData := map[string]interface{}{
		"user_id":   req.UserID,
		"job_type":  req.JobType,
		"data_type": req.DataType,
		"status":    "pending",
		"priority":  req.Priority,
	}

	var result []map[string]interface{}
	_, err = s.client.From("jobs").Insert(jobData, false, "", "", "").ExecuteTo(&result)
	if err != nil {
		logger.Error("enqueue_job", "Failed to insert job", err, nil)
		return nil, nil, err
	}

	if len(result) == 0 {
		logger.Error("enqueue_job", "No job returned after insert", nil, nil)
		return nil, nil, fmt.Errorf("failed to create job")
	}

	job := &models.Job{
		ID:         result[0]["id"].(string),
		UserID:     req.UserID,
		JobType:    req.JobType,
		DataType:   req.DataType,
		Status:     "pending",
		Priority:   req.Priority,
		RetryCount: 0,
	}

	if createdAtStr, ok := result[0]["created_at"].(string); ok {
		if createdAt, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			job.CreatedAt = createdAt
		}
	}

	logger.Info("enqueue_job", "Job enqueued successfully", map[string]interface{}{
		"job_id":    job.ID,
		"user_id":   req.UserID,
		"job_type":  req.JobType,
		"data_type": req.DataType,
	})
	return job, nil, nil
}

// CountRunningLoginJobs counts currently running login jobs (for global limit)
func (s *SupabaseClient) CountRunningLoginJobs() (int, error) {
	var result []map[string]interface{}
	_, err := s.client.From("jobs").
		Select("id", "", false).
		Eq("job_type", "login").
		Eq("status", "running").
		ExecuteTo(&result)

	if err != nil {
		return 0, err
	}

	return len(result), nil
}

// ClaimNextJob atomically claims the next pending job for execution
func (s *SupabaseClient) ClaimNextJob() (*models.Job, error) {

	// First, find the next job to claim
	var jobsResult []map[string]interface{}
	_, err := s.client.From("jobs").
		Select("*", "", false).
		Eq("status", "pending").
		Neq("data_type", models.AttendanceDataType).
		Order("priority", &postgrest.OrderOpts{Ascending: false}).
		Order("created_at", &postgrest.OrderOpts{Ascending: true}).
		Limit(1, "").
		ExecuteTo(&jobsResult)

	if err != nil {
		logger.Error("claim_next_job", "Failed to find next job", err, nil)
		return nil, err
	}

	if len(jobsResult) == 0 {
		// No pending jobs
		return nil, nil
	}

	jobData := jobsResult[0]
	jobID := jobData["id"].(string)

	// Atomically claim the job
	now := time.Now().Format(time.RFC3339)
	updateData := map[string]interface{}{
		"status":     "running",
		"started_at": now,
	}

	var updateResult []map[string]interface{}
	_, err = s.client.From("jobs").
		Update(updateData, "", "").
		Eq("id", jobID).
		Eq("status", "pending"). // Atomic check
		ExecuteTo(&updateResult)

	if err != nil {
		logger.Error("claim_next_job", "Failed to claim job", err, nil)
		return nil, err
	}

	if len(updateResult) == 0 {
		// Another worker claimed it first
		logger.Info("claim_next_job", "Job was claimed by another worker", map[string]interface{}{
			"job_id": jobID,
		})
		return nil, nil
	}

	// Parse the claimed job
	job := &models.Job{
		ID:         jobID,
		UserID:     jobData["user_id"].(string),
		JobType:    jobData["job_type"].(string),
		DataType:   jobData["data_type"].(string),
		Status:     "running",
		Priority:   int(jobData["priority"].(float64)),
		RetryCount: int(jobData["retry_count"].(float64)),
	}

	if createdAtStr, ok := jobData["created_at"].(string); ok {
		if createdAt, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			job.CreatedAt = createdAt
		}
	}

	if startedAtStr, ok := jobData["started_at"].(string); ok {
		if startedAt, err := time.Parse(time.RFC3339, startedAtStr); err == nil {
			job.StartedAt = &startedAt
		}
	}

	if failureReason, ok := jobData["failure_reason"].(string); ok {
		job.FailureReason = &failureReason
	}

	logger.Info("claim_next_job", "Job claimed successfully", map[string]interface{}{
		"job_id":    job.ID,
		"user_id":   job.UserID,
		"job_type":  job.JobType,
		"data_type": job.DataType,
		"priority":  job.Priority,
	})
	return job, nil
}

// ClaimNextAttendanceJob claims only attendance jobs for the dedicated scheduler worker.
func (s *SupabaseClient) ClaimNextAttendanceJob() (*models.Job, error) {

	var jobsResult []map[string]interface{}
	_, err := s.client.From("jobs").
		Select("*", "", false).
		Eq("status", "pending").
		Eq("data_type", models.AttendanceDataType).
		Order("priority", &postgrest.OrderOpts{Ascending: false}).
		Order("created_at", &postgrest.OrderOpts{Ascending: true}).
		Limit(1, "").
		ExecuteTo(&jobsResult)

	if err != nil {
		logger.Error("claim_attendance_job", "Failed to find next attendance job", err, nil)
		return nil, err
	}

	if len(jobsResult) == 0 {
		return nil, nil
	}

	jobData := jobsResult[0]
	jobID := jobData["id"].(string)

	now := time.Now().Format(time.RFC3339)
	updateData := map[string]interface{}{
		"status":     "running",
		"started_at": now,
	}

	var updateResult []map[string]interface{}
	_, err = s.client.From("jobs").
		Update(updateData, "", "").
		Eq("id", jobID).
		Eq("status", "pending").
		ExecuteTo(&updateResult)

	if err != nil {
		logger.Error("claim_attendance_job", "Failed to claim attendance job", err, nil)
		return nil, err
	}

	if len(updateResult) == 0 {
		logger.Info("claim_attendance_job", "Attendance job claimed by another worker", map[string]interface{}{
			"job_id": jobID,
		})
		return nil, nil
	}

	job := &models.Job{
		ID:         jobID,
		UserID:     jobData["user_id"].(string),
		JobType:    jobData["job_type"].(string),
		DataType:   jobData["data_type"].(string),
		Status:     "running",
		Priority:   int(jobData["priority"].(float64)),
		RetryCount: int(jobData["retry_count"].(float64)),
	}

	if createdAtStr, ok := jobData["created_at"].(string); ok {
		if createdAt, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			job.CreatedAt = createdAt
		}
	}

	if startedAtStr, ok := jobData["started_at"].(string); ok {
		if startedAt, err := time.Parse(time.RFC3339, startedAtStr); err == nil {
			job.StartedAt = &startedAt
		}
	}

	if failureReason, ok := jobData["failure_reason"].(string); ok {
		job.FailureReason = &failureReason
	}

	logger.Info("claim_attendance_job", "Attendance job claimed successfully", map[string]interface{}{
		"job_id":    job.ID,
		"user_id":   job.UserID,
		"job_type":  job.JobType,
		"data_type": job.DataType,
		"priority":  job.Priority,
	})

	return job, nil
}

// UpdateJobStatus updates a job's status and optionally failure reason
func (s *SupabaseClient) UpdateJobStatus(jobID, status string, failureReason *string) error {
	logger.Info("update_job_status", "Updating job status", map[string]interface{}{
		"job_id":         jobID,
		"status":         status,
		"failure_reason": failureReason,
	})

	updateData := map[string]interface{}{
		"status": status,
	}

	if failureReason != nil {
		updateData["failure_reason"] = *failureReason
	}

	var result []map[string]interface{}
	_, err := s.client.From("jobs").
		Update(updateData, "", "").
		Eq("id", jobID).
		ExecuteTo(&result)

	if err != nil {
		logger.Error("update_job_status", "Failed to update job status", err, nil)
		return err
	}

	logger.Info("update_job_status", "Job status updated successfully", map[string]interface{}{
		"job_id": jobID,
		"status": status,
	})
	return nil
}

// IncrementJobRetry increments retry count for a job
func (s *SupabaseClient) IncrementJobRetry(jobID string) error {
	logger.Info("increment_job_retry", "Incrementing job retry count", map[string]interface{}{
		"job_id": jobID,
	})

	var result []map[string]interface{}
	// Get current retry count
	var currentResult []map[string]interface{}
	_, err := s.client.From("jobs").
		Select("retry_count", "", false).
		Eq("id", jobID).
		ExecuteTo(&currentResult)

	if err != nil || len(currentResult) == 0 {
		return err
	}

	currentRetry := int(currentResult[0]["retry_count"].(float64))

	_, err = s.client.From("jobs").
		Update(map[string]interface{}{
			"retry_count": currentRetry + 1,
		}, "", "").
		Eq("id", jobID).
		ExecuteTo(&result)

	if err != nil {
		logger.Error("increment_job_retry", "Failed to increment retry count", err, nil)
		return err
	}

	logger.Info("increment_job_retry", "Retry count incremented", map[string]interface{}{
		"job_id": jobID,
	})
	return nil
}

// EnqueueSpecificFetchJobs creates fetch jobs for only the requested data types after successful login
func (s *SupabaseClient) EnqueueSpecificFetchJobs(userID string, requestedDataTypes []string) error {
	logger.Info("enqueue_specific_fetch_jobs", "Enqueueing specific fetch jobs after login", map[string]interface{}{
		"user_id":              userID,
		"requested_data_types": requestedDataTypes,
	})

	successCount := 0

	for _, dataType := range requestedDataTypes {
		req := models.JobCreateRequest{
			UserID:   userID,
			JobType:  "fetch",
			DataType: dataType,
			Priority: 10, // Fetch jobs are priority 10
		}

		_, _, err := s.EnqueueJob(req)
		if err != nil && err.Error() != "job already exists" {
			logger.Error("enqueue_specific_fetch_jobs", "Failed to enqueue fetch job", err, map[string]interface{}{
				"user_id":   userID,
				"data_type": dataType,
			})
			// Continue with other jobs even if one fails
		} else {
			successCount++
		}
	}

	logger.Info("enqueue_specific_fetch_jobs", "Specific fetch jobs enqueued", map[string]interface{}{
		"user_id":    userID,
		"requested":  len(requestedDataTypes),
		"successful": successCount,
		"failed":     len(requestedDataTypes) - successCount,
	})
	return nil
}

// EnqueueDependentFetchJobs creates fetch jobs after successful login (deprecated - use EnqueueSpecificFetchJobs)
func (s *SupabaseClient) EnqueueDependentFetchJobs(userID string) error {
	return s.EnqueueSpecificFetchJobs(userID, []string{"courses", "timetable", "calendar", "user"})
}
