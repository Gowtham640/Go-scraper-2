package worker

import (
	"fmt"
	"srm-academia-scraper/auth"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/scraper"
	"srm-academia-scraper/storage"
	"strings"
	"time"
)

// Worker manages background job execution
type Worker struct {
	db             *storage.SupabaseClient
	sessionManager *auth.SessionManager
	loginChan      chan models.WorkerLoginRequest
}

// NewWorker creates a new background worker
func NewWorker(db *storage.SupabaseClient) *Worker {
	return &Worker{
		db:             db,
		sessionManager: auth.NewSessionManager(db),
		loginChan:      make(chan models.WorkerLoginRequest, 100), // Buffer for login requests
	}
}

// Start begins the background job processing loop
func (w *Worker) Start() {
	logger.Info("worker_start", "Starting background worker", nil)

	go func() {
		for {
			select {
			case loginReq := <-w.loginChan:
				w.processLoginRequest(loginReq)
			default:
				w.processNextJob()
			}
			time.Sleep(1 * time.Second) // Prevent tight loop
		}
	}()
}

// EnqueueLoginRequest adds a login request to the queue
func (w *Worker) EnqueueLoginRequest(req models.WorkerLoginRequest) {
	select {
	case w.loginChan <- req:
		logger.Info("enqueue_login_request", "Login request enqueued", map[string]interface{}{
			"user_id": req.UserID,
			"email":   req.Email,
		})
	default:
		logger.Error("enqueue_login_request", "Login channel full, dropping request", nil, map[string]interface{}{
			"user_id": req.UserID,
		})
	}
}

// processNextJob claims and executes the next pending job
func (w *Worker) processNextJob() {
	// Claim next job
	job, err := w.db.ClaimNextJob()
	if err != nil {
		logger.Error("worker_process", "Failed to claim job", err, nil)
		return
	}

	if job == nil {
		// No jobs available
		return
	}

	logger.Info("worker_process", "Processing job", map[string]interface{}{
		"job_id":    job.ID,
		"user_id":   job.UserID,
		"job_type":  job.JobType,
		"data_type": job.DataType,
		"priority":  job.Priority,
	})

	// Execute job based on type
	var success bool
	var failureReason *string

	switch job.JobType {
	case "fetch":
		success, failureReason = w.executeFetchJob(job)
	default:
		logger.Error("worker_process", "Unknown job type", nil, map[string]interface{}{
			"job_type": job.JobType,
		})
		failureReason = stringPtr("Unknown job type")
		success = false
	}

	// Update job status
	var newStatus string
	if success {
		newStatus = "done"
	} else {
		// Check retry logic
		if job.RetryCount < 2 {
			logger.Info("worker_process", "Job failed, retrying", map[string]interface{}{
				"job_id":      job.ID,
				"retry_count": job.RetryCount,
			})
			// Increment retry count and reset to pending
			err := w.db.IncrementJobRetry(job.ID)
			if err != nil {
				logger.Error("worker_process", "Failed to increment retry count", err, map[string]interface{}{
					"job_id": job.ID,
				})
			}
			// Reset to pending for retry
			newStatus = "pending"
		} else {
			logger.Info("worker_process", "Job failed permanently", map[string]interface{}{
				"job_id":        job.ID,
				"retry_count":   job.RetryCount,
				"failure_reason": failureReason,
			})
			newStatus = "failed"
		}
	}

	err = w.db.UpdateJobStatus(job.ID, newStatus, failureReason)
	if err != nil {
		logger.Error("worker_process", "Failed to update job status", err, map[string]interface{}{
			"job_id": job.ID,
		})
	}
}

// processLoginRequest processes a login request with credentials
func (w *Worker) processLoginRequest(req models.WorkerLoginRequest) {
	logger.Info("process_login_request", "Processing login request", map[string]interface{}{
		"user_id": req.UserID,
		"email":   req.Email,
	})

	// Execute login
	success, failureReason := w.executeLoginWithCredentials(req.UserID, req.Email, req.Password)

	if success {
		logger.Info("process_login_request", "Login successful, enqueuing requested fetch jobs", map[string]interface{}{
			"user_id":              req.UserID,
			"requested_data_types": req.RequestedDataTypes,
		})

		// Enqueue only the requested fetch jobs after successful login
		err := w.db.EnqueueSpecificFetchJobs(req.UserID, req.RequestedDataTypes)
		if err != nil {
			logger.Error("process_login_request", "Failed to enqueue requested jobs", err, map[string]interface{}{
				"user_id": req.UserID,
			})
		}
	} else {
		logger.Error("process_login_request", "Login failed", nil, map[string]interface{}{
			"user_id":       req.UserID,
			"failure_reason": failureReason,
		})
	}
}

// executeLoginWithCredentials executes login with provided credentials
func (w *Worker) executeLoginWithCredentials(userID, email, password string) (bool, *string) {
	logger.Info("execute_login_with_credentials", "Executing login with credentials", map[string]interface{}{
		"user_id": userID,
		"email":   email,
	})

	// Check global Playwright limit
	runningCount, err := w.db.CountRunningLoginJobs()
	if err != nil {
		failureMsg := "Failed to check running login jobs"
		logger.Error("execute_login_with_credentials", failureMsg, err, nil)
		return false, &failureMsg
	}

	if runningCount >= 3 {
		failureMsg := "Playwright limit reached"
		logger.Warn("execute_login_with_credentials", failureMsg, map[string]interface{}{
			"running_count": runningCount,
		})
		return false, &failureMsg
	}

	// Perform browser login
	cookies, err := w.sessionManager.PerformBrowserLogin(userID, email, password)
	if err != nil {
		failureMsg := fmt.Sprintf("Browser login failed: %v", err)
		logger.Error("execute_login_with_credentials", failureMsg, err, map[string]interface{}{
			"user_id": userID,
		})
		return false, &failureMsg
	}

	// Store tokens
	expiryDays := auth.ExtractExpiryDays(cookies)
	err = w.db.UpsertToken(userID, email, cookies, expiryDays)
	if err != nil {
		failureMsg := fmt.Sprintf("Token storage failed: %v", err)
		logger.Error("execute_login_with_credentials", failureMsg, err, map[string]interface{}{
			"user_id": userID,
		})
		return false, &failureMsg
	}

	logger.Info("execute_login_with_credentials", "Login completed successfully", map[string]interface{}{
		"user_id":     userID,
		"expiry_days": expiryDays,
	})

	return true, nil
}

// executeFetchJob executes a fetch job using stored tokens
func (w *Worker) executeFetchJob(job *models.Job) (bool, *string) {
	logger.Info("execute_fetch_job", "Executing fetch job", map[string]interface{}{
		"job_id":    job.ID,
		"user_id":   job.UserID,
		"data_type": job.DataType,
	})

	// Get stored token
	tokenData, err := w.db.GetToken(job.UserID)
	if err != nil {
		failureMsg := "No token found"
		logger.Error("execute_fetch_job", failureMsg, err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		return false, &failureMsg
	}

	// Execute fetch based on data type
	switch job.DataType {
	case "courses":
		return w.fetchCourses(job, tokenData)
	case "timetable":
		return w.fetchTimetable(job, tokenData)
	case "calendar":
		return w.fetchCalendar(job, tokenData)
	case "user":
		return w.fetchUserInfo(job, tokenData)
	default:
		failureMsg := fmt.Sprintf("Unknown data type: %s", job.DataType)
		logger.Error("execute_fetch_job", failureMsg, nil, map[string]interface{}{
			"job_id":    job.ID,
			"data_type": job.DataType,
		})
		return false, &failureMsg
	}
}

// fetchCourses fetches course data
func (w *Worker) fetchCourses(job *models.Job, tokenData *models.TokenData) (bool, *string) {
	logger.Info("fetch_courses", "Fetching courses", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
	})

	// Note: Batch is no longer needed here since timetable generation moved to fetchTimetable

	// Fetch HTML
	scraper.RateLimit(1 * time.Second)
	httpClient := scraper.NewHTTPClient()
	htmlContent, err := httpClient.GetWithCookies(scraper.TimeTableURL, tokenData.Tokens)

	// Check for auth failure
	if err != nil && w.isAuthFailure(err) {
		logger.Info("fetch_courses", "Auth failure detected, will trigger login", map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		// Enqueue login job
		jobReq := models.JobCreateRequest{
			UserID:   job.UserID,
			JobType:  "login",
			DataType: "auth",
			Priority: 50, // Token refresh
		}
		w.db.EnqueueJob(jobReq) // Ignore error
		failureMsg := "Authentication failed"
		return false, &failureMsg
	}

	if err != nil {
		failureMsg := fmt.Sprintf("HTTP request failed: %v", err)
		return false, &failureMsg
	}

	// Extract and parse HTML
	actualHTML, err := scraper.ExtractHTMLFromSanitizer(string(htmlContent), "courses.html")
	if err != nil {
		failureMsg := fmt.Sprintf("HTML extraction failed: %v", err)
		return false, &failureMsg
	}

	// Parse user info first
	userInfo, err := scraper.ParseUserInfo(actualHTML)
	if err != nil {
		failureMsg := fmt.Sprintf("User info parsing failed: %v", err)
		return false, &failureMsg
	}

	// Parse courses
	coursesData, err := scraper.ParseCourses(string(htmlContent), userInfo.RegNumber)
	if err != nil {
		failureMsg := fmt.Sprintf("Courses parsing failed: %v", err)
		return false, &failureMsg
	}

	// Clean data
	scraper.CleanCoursesData(coursesData)

	// Store in cache
	err = w.db.UpsertUserCache(job.UserID, "courses", coursesData, 24)
	if err != nil {
		failureMsg := fmt.Sprintf("Cache storage failed: %v", err)
		return false, &failureMsg
	}

	// Note: Timetable generation is now handled separately in fetchTimetable

	logger.Info("fetch_courses", "Courses fetched successfully", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
		"count":   len(coursesData.Courses),
	})

	return true, nil
}

// fetchTimetable fetches timetable data (actually generated from courses)
func (w *Worker) fetchTimetable(job *models.Job, tokenData *models.TokenData) (bool, *string) {
	logger.Info("fetch_timetable", "Fetching timetable", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
	})

	// First ensure we have user data to get batch
	userBatch, err := w.db.GetUserBatch(job.UserID)
	if err != nil {
		logger.Warn("fetch_timetable", "User batch not found, attempting to fetch user data first", map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
			"error":   err.Error(),
		})

		// Try to fetch user data first
		userJob := &models.Job{
			ID:     job.ID + "_user", // Temporary ID for user fetch
			UserID: job.UserID,
			DataType: "user",
		}
		success, failureMsg := w.fetchUserInfo(userJob, tokenData)
		if !success {
			return false, failureMsg
		}

		// Now try to get batch again
		userBatch, err = w.db.GetUserBatch(job.UserID)
		if err != nil {
			userBatch = "2" // Default batch
			logger.Warn("fetch_timetable", "Using default batch after user fetch", map[string]interface{}{
				"job_id":  job.ID,
				"user_id": job.UserID,
				"batch":   userBatch,
			})
		}
	}

	// Check if we have courses data cached
	coursesData, err := w.db.GetUserCache(job.UserID, "courses")
	if err != nil {
		logger.Info("fetch_timetable", "Courses data not cached, fetching courses first", map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})

		// Fetch courses data first
		coursesJob := &models.Job{
			ID:     job.ID + "_courses", // Temporary ID for courses fetch
			UserID: job.UserID,
			DataType: "courses",
		}
		success, failureMsg := w.fetchCourses(coursesJob, tokenData)
		if !success {
			return false, failureMsg
		}

		// Get courses data again
		coursesData, err = w.db.GetUserCache(job.UserID, "courses")
		if err != nil {
			failureMsg := "Failed to retrieve courses data after fetch"
			return false, &failureMsg
		}
	}

	// Now generate timetable from courses data
	coursesDataTyped, ok := coursesData.(models.CoursesData)
	if !ok {
		failureMsg := "Invalid courses data type"
		return false, &failureMsg
	}

	timetableData, err := scraper.GenerateTimetable(coursesDataTyped.Courses, userBatch)
	if err != nil {
		failureMsg := fmt.Sprintf("Timetable generation failed: %v", err)
		return false, &failureMsg
	}

	timetableData.RegNumber = coursesDataTyped.RegNumber
	timetableData.Batch = userBatch

	// Store timetable in cache
	err = w.db.UpsertUserCache(job.UserID, "timetable", timetableData, 24*7) // 7 days for timetable
	if err != nil {
		failureMsg := fmt.Sprintf("Timetable cache storage failed: %v", err)
		return false, &failureMsg
	}

	logger.Info("fetch_timetable", "Timetable fetched successfully", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
		"batch":   userBatch,
	})

	return true, nil
}

// fetchCalendar fetches calendar data
func (w *Worker) fetchCalendar(job *models.Job, tokenData *models.TokenData) (bool, *string) {
	logger.Info("fetch_calendar", "Fetching calendar", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
	})

	// Fetch HTML
	scraper.RateLimit(1 * time.Second)
	httpClient := scraper.NewHTTPClient()
	htmlContent, err := httpClient.GetWithCookies(scraper.CalendarURL, tokenData.Tokens)

	// Check for auth failure
	if err != nil && w.isAuthFailure(err) {
		logger.Info("fetch_calendar", "Auth failure detected, will trigger login", map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		// Enqueue login job
		jobReq := models.JobCreateRequest{
			UserID:   job.UserID,
			JobType:  "login",
			DataType: "auth",
			Priority: 50, // Token refresh
		}
		w.db.EnqueueJob(jobReq) // Ignore error
		failureMsg := "Authentication failed"
		return false, &failureMsg
	}

	if err != nil {
		failureMsg := fmt.Sprintf("HTTP request failed: %v", err)
		return false, &failureMsg
	}

	// Decode HTML entities
	actualHTML := scraper.DecodeHTMLEntities(string(htmlContent))

	// Parse calendar
	calendarData, err := scraper.ParseCalendar(actualHTML)
	if err != nil {
		failureMsg := fmt.Sprintf("Calendar parsing failed: %v", err)
		return false, &failureMsg
	}

	// Clean and normalize
	scraper.CleanCalendarData(calendarData)
	normalizedCalendar := scraper.NormalizeCalendarData(calendarData)

	// Store in global calendar table
	err = w.db.UpsertCalendar("Default", 0, normalizedCalendar)
	if err != nil {
		failureMsg := fmt.Sprintf("Calendar storage failed: %v", err)
		return false, &failureMsg
	}

	// Also cache per-user
	err = w.db.UpsertUserCache(job.UserID, "calendar", calendarData, 24)
	if err != nil {
		failureMsg := fmt.Sprintf("User cache storage failed: %v", err)
		return false, &failureMsg
	}

	logger.Info("fetch_calendar", "Calendar fetched successfully", map[string]interface{}{
		"job_id": job.ID,
		"user_id": job.UserID,
	})

	return true, nil
}

// fetchUserInfo fetches user profile data
func (w *Worker) fetchUserInfo(job *models.Job, tokenData *models.TokenData) (bool, *string) {
	logger.Info("fetch_user_info", "Fetching user info", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
	})

	// Fetch HTML
	scraper.RateLimit(1 * time.Second)
	httpClient := scraper.NewHTTPClient()
	htmlContent, err := httpClient.GetWithCookies(scraper.TimeTableURL, tokenData.Tokens)

	// Check for auth failure
	if err != nil && w.isAuthFailure(err) {
		logger.Info("fetch_user_info", "Auth failure detected, will trigger login", map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		// Enqueue login job
		jobReq := models.JobCreateRequest{
			UserID:   job.UserID,
			JobType:  "login",
			DataType: "auth",
			Priority: 50, // Token refresh
		}
		w.db.EnqueueJob(jobReq) // Ignore error
		failureMsg := "Authentication failed"
		return false, &failureMsg
	}

	if err != nil {
		failureMsg := fmt.Sprintf("HTTP request failed: %v", err)
		return false, &failureMsg
	}

	// Extract HTML
	actualHTML, err := scraper.ExtractHTMLFromSanitizer(string(htmlContent), "users.html")
	if err != nil {
		failureMsg := fmt.Sprintf("HTML extraction failed: %v", err)
		return false, &failureMsg
	}

	// Parse user info
	userInfo, err := scraper.ParseUserInfo(actualHTML)
	if err != nil {
		failureMsg := fmt.Sprintf("User info parsing failed: %v", err)
		return false, &failureMsg
	}

	// Store in users table
	err = w.db.UpsertUser(job.UserID, "", userInfo) // Email not needed for update
	if err != nil {
		failureMsg := fmt.Sprintf("User storage failed: %v", err)
		return false, &failureMsg
	}

	// Also cache
	err = w.db.UpsertUserCache(job.UserID, "user", userInfo, 24)
	if err != nil {
		failureMsg := fmt.Sprintf("User cache storage failed: %v", err)
		return false, &failureMsg
	}

	logger.Info("fetch_user_info", "User info fetched successfully", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
		"name":    userInfo.Name,
	})

	return true, nil
}

// isAuthFailure checks if an error indicates authentication failure
func (w *Worker) isAuthFailure(err error) bool {
	errStr := err.Error()
	return strings.Contains(errStr, "401") ||
		strings.Contains(errStr, "403") ||
		strings.Contains(errStr, "login") ||
		strings.Contains(errStr, "auth")
}

// stringPtr creates a string pointer
func stringPtr(s string) *string {
	return &s
}