package worker

import (
	"fmt"
	"os"
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

	// Dedicated goroutine for login requests retains prior behavior via loginChan.
	logger.Info("worker_start", "Login worker goroutine started", map[string]interface{}{"log_type": "login"})
	go func() {
		for {
			loginReq := <-w.loginChan
			w.processLoginRequest(loginReq)
		}
	}()

	// Spawn three independent fetch-job workers that reuse existing processNextJob logic.
	for i := 0; i < 3; i++ {
		workerID := i + 1
		logger.Info("worker_start", "Fetch worker goroutine started", map[string]interface{}{
			"log_type":  "fetch",
			"worker_id": workerID,
		})
		go func() {
			for {
				w.processNextJob()
				time.Sleep(1 * time.Second) // Prevent tight loop
			}
		}()
	}

	go func() {
		logger.Info("attendance_worker", "Dedicated attendance worker started", map[string]interface{}{"log_type": "attendance"})
		for {
			w.processNextAttendanceJob()
			time.Sleep(2 * time.Second) // keep scheduling intervals
		}
	}()

	// Periodically log claim attempts so we avoid flooding logs while still showing activity.
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		logger.Info("claim_next_job", "Attempting to claim next job", nil)
		for range ticker.C {
			logger.Info("claim_next_job", "Attempting to claim next job", nil)
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
	job, err := w.db.ClaimNextJob()
	if err != nil {
		logger.Error("worker_process", "Failed to claim job", err, nil)
		return
	}
	if job == nil {
		return
	}
	w.processClaimedJob(job)
}

func (w *Worker) processNextAttendanceJob() {
	job, err := w.db.ClaimNextAttendanceJob()
	if err != nil {
		logger.Error("attendance_worker", "Failed to claim attendance job", err, nil)
		return
	}
	if job == nil {
		return
	}
	w.processClaimedJob(job)
	logger.Info("attendance_worker", "Attendance job processed", map[string]interface{}{
		"job_id":   job.ID,
		"user_id":  job.UserID,
		"priority": job.Priority,
	})
}

func (w *Worker) processClaimedJob(job *models.Job) {
	logger.Info("worker_process", "Processing job", map[string]interface{}{
		"job_id":    job.ID,
		"user_id":   job.UserID,
		"job_type":  job.JobType,
		"data_type": job.DataType,
		"priority":  job.Priority,
	})

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

	var newStatus string
	if success {
		newStatus = "done"
	} else {
		if job.RetryCount < 2 {
			logger.Info("worker_process", "Job failed, retrying", map[string]interface{}{
				"job_id":      job.ID,
				"retry_count": job.RetryCount,
			})
			err := w.db.IncrementJobRetry(job.ID)
			if err != nil {
				logger.Error("worker_process", "Failed to increment retry count", err, map[string]interface{}{
					"job_id": job.ID,
				})
			}
			newStatus = "pending"
		} else {
			logger.Info("worker_process", "Job failed permanently", map[string]interface{}{
				"job_id":         job.ID,
				"retry_count":    job.RetryCount,
				"failure_reason": failureReason,
			})
			newStatus = "failed"
		}
	}

	err := w.db.UpdateJobStatus(job.ID, newStatus, failureReason)
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
			"user_id":        req.UserID,
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

	if saveErr := w.db.SaveUserEncryptedPassword(userID, email, password); saveErr != nil {
		logger.Error("execute_login_with_credentials", "Failed to store encrypted password", saveErr, map[string]interface{}{
			"user_id": userID,
		})
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
	case "attendance":
		return w.fetchAttendance(job, tokenData)
	case "marks":
		return w.fetchMarks(job, tokenData)
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
			ID:       job.ID + "_user", // Temporary ID for user fetch
			UserID:   job.UserID,
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
			ID:       job.ID + "_courses", // Temporary ID for courses fetch
			UserID:   job.UserID,
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
	err = w.db.UpsertUserCache(job.UserID, "timetable", timetableData, 24*30) // 1 month for timetable
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
		"job_id":  job.ID,
		"user_id": job.UserID,
	})

	return true, nil
}

// fetchAttendance fetches attendance data
func (w *Worker) fetchAttendance(job *models.Job, tokenData *models.TokenData) (bool, *string) {
	logger.Info("fetch_attendance", "Starting attendance fetch process", map[string]interface{}{
		"job_id":   job.ID,
		"user_id":  job.UserID,
		"endpoint": scraper.AttendanceURL,
	})

	// Spread out attendance fetches to reduce portal load.
	time.Sleep(750 * time.Millisecond)

	// Apply rate limiting
	logger.Info("fetch_attendance", "Applying rate limit before request", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
		"delay":   "1 second",
	})
	scraper.RateLimit(1 * time.Second)

	// Create HTTP client
	logger.Info("fetch_attendance", "Creating HTTP client", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
	})
	httpClient := scraper.NewHTTPClient()

	// Fetch HTML from attendance endpoint
	logger.Info("fetch_attendance", "Making HTTP request to attendance endpoint", map[string]interface{}{
		"job_id":      job.ID,
		"user_id":     job.UserID,
		"url":         scraper.AttendanceURL,
		"has_cookies": tokenData != nil && tokenData.Tokens != "",
	})
	htmlContent, err := httpClient.GetWithCookies(scraper.AttendanceURL, tokenData.Tokens)

	// Check for authentication failure
	if err != nil && w.isAuthFailure(err) {
		logger.Warn("fetch_attendance", "Authentication failure detected, triggering login", map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
			"error":   err.Error(),
		})

		// Enqueue login job
		jobReq := models.JobCreateRequest{
			UserID:   job.UserID,
			JobType:  "login",
			DataType: "auth",
			Priority: 50, // Token refresh
		}
		w.db.EnqueueJob(jobReq) // Ignore error

		failureMsg := "Authentication failed - login job enqueued"
		logger.Info("fetch_attendance", failureMsg, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		return false, &failureMsg
	}

	// Check for general HTTP errors
	if err != nil {
		failureMsg := fmt.Sprintf("HTTP request failed: %v", err)
		logger.Error("fetch_attendance", failureMsg, err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
			"url":     scraper.AttendanceURL,
		})
		return false, &failureMsg
	}

	// Log successful fetch
	logger.Info("fetch_attendance", "HTML content successfully fetched", map[string]interface{}{
		"job_id":       job.ID,
		"user_id":      job.UserID,
		"content_size": len(htmlContent),
		"url":          scraper.AttendanceURL,
	})

	// Save HTML content to attendance.html file
	logger.Info("fetch_attendance", "Saving HTML content to attendance.html file", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
		"file":    "attendance.html",
	})
	err = os.WriteFile("attendance.html", htmlContent, 0644)
	if err != nil {
		logger.Error("fetch_attendance", "Failed to save HTML to file", err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
			"file":    "attendance.html",
		})
		// Continue even if file save fails
	} else {
		logger.Info("fetch_attendance", "HTML content successfully saved to file", map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
			"file":    "attendance.html",
			"bytes":   len(htmlContent),
		})
	}

	rawHTML := string(htmlContent)
	decodedHTML, err := scraper.ExtractSanitizedHTML(rawHTML)
	if err != nil {
		if isLikelyLoginPage(rawHTML) {
			logger.Warn("fetch_attendance", "Detected login page instead of attendance data", map[string]interface{}{
				"job_id":  job.ID,
				"user_id": job.UserID,
				"snippet": snippet(rawHTML),
			})
			w.enqueueAttendanceLogin(job.UserID)
			failureMsg := "Login page detected while fetching attendance"
			return false, &failureMsg
		}

		failureMsg := fmt.Sprintf("Failed to extract sanitized HTML: %v", err)
		logger.Error("fetch_attendance", failureMsg, err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		return false, &failureMsg
	}

	logger.Info("fetch_attendance", "Sanitized HTML extracted", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
		"length":  len(decodedHTML),
	})

	attendanceEntries, err := scraper.ParseAttendance(decodedHTML)
	if err != nil {
		failureMsg := fmt.Sprintf("Failed to parse attendance data: %v", err)
		logger.Error("fetch_attendance", failureMsg, err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		return false, &failureMsg
	}

	logger.Info("fetch_attendance", "Parsed attendance entries", map[string]interface{}{
		"job_id":      job.ID,
		"user_id":     job.UserID,
		"entry_count": len(attendanceEntries),
	})

	fetchedAt := time.Now().UTC()
	attendanceData := map[string]interface{}{
		"entries":    attendanceEntries,
		"url":        scraper.AttendanceURL,
		"fetched_at": fetchedAt.Format(time.RFC3339),
	}

	// Store in database cache
	logger.Info("fetch_attendance", "Storing attendance data in cache", map[string]interface{}{
		"job_id":    job.ID,
		"user_id":   job.UserID,
		"cache_ttl": 2,
		"data_type": "attendance",
	})
	if err = w.db.UpsertUserCache(job.UserID, "attendance", attendanceData, 2); err != nil {
		failureMsg := fmt.Sprintf("Cache storage failed: %v", err)
		logger.Error("fetch_attendance", failureMsg, err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		return false, &failureMsg
	}

	processor := newAttendanceProcessor(w.db)
	if err := processor.Process(job.ID, job.UserID, attendanceEntries, fetchedAt); err != nil {
		failureMsg := fmt.Sprintf("Attendance pipeline failed: %v", err)
		logger.Error("fetch_attendance", failureMsg, err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		return false, &failureMsg
	}

	logger.Info("fetch_attendance", "Attendance fetch process completed successfully", map[string]interface{}{
		"job_id":        job.ID,
		"user_id":       job.UserID,
		"cached":        true,
		"file_saved":    true,
		"pipeline_done": true,
	})

	return true, nil
}

func (w *Worker) enqueueAttendanceLogin(userID string) {
	jobReq := models.JobCreateRequest{
		UserID:   userID,
		JobType:  "login",
		DataType: "auth",
		Priority: 50,
	}
	if _, _, err := w.db.EnqueueJob(jobReq); err != nil && err.Error() != "job already exists" {
		logger.Warn("fetch_attendance", "Failed to enqueue login job for attendance recovery", map[string]interface{}{
			"user_id": userID,
			"error":   err.Error(),
		})
	}
}

func isLikelyLoginPage(body string) bool {
	lowered := strings.ToLower(body)
	return (strings.Contains(lowered, "login") && strings.Contains(lowered, "password")) ||
		strings.Contains(lowered, "iamcsr") ||
		strings.Contains(lowered, "auth") && strings.Contains(lowered, "token")
}

func snippet(body string) string {
	const max = 200
	if len(body) <= max {
		return body
	}
	return body[:max]
}

// fetchMarks fetches marks data
func (w *Worker) fetchMarks(job *models.Job, tokenData *models.TokenData) (bool, *string) {
	logger.Info("fetch_marks", "Fetching marks", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
	})

	// Apply rate limiting
	logger.Info("fetch_marks", "Applying rate limit before request", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
		"delay":   "1 second",
	})
	scraper.RateLimit(1 * time.Second)

	logger.Info("fetch_marks", "Creating HTTP client", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
	})
	httpClient := scraper.NewHTTPClient()

	logger.Info("fetch_marks", "Making HTTP request to attendance endpoint", map[string]interface{}{
		"job_id":      job.ID,
		"user_id":     job.UserID,
		"url":         scraper.AttendanceURL,
		"has_cookies": tokenData != nil && tokenData.Tokens != "",
	})
	htmlContent, err := httpClient.GetWithCookies(scraper.AttendanceURL, tokenData.Tokens)

	// Check for auth failure
	if err != nil && w.isAuthFailure(err) {
		logger.Info("fetch_marks", "Auth failure detected, will trigger login", map[string]interface{}{
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
		logger.Error("fetch_marks", failureMsg, err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
			"url":     scraper.AttendanceURL,
		})
		return false, &failureMsg
	}

	logger.Info("fetch_marks", "HTML content successfully fetched", map[string]interface{}{
		"job_id":       job.ID,
		"user_id":      job.UserID,
		"content_size": len(htmlContent),
		"url":          scraper.AttendanceURL,
	})

	decodedHTML, err := scraper.ExtractSanitizedHTML(string(htmlContent))
	if err != nil {
		failureMsg := fmt.Sprintf("Failed to extract sanitized HTML: %v", err)
		logger.Error("fetch_marks", failureMsg, err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		return false, &failureMsg
	}

	marksEntries, err := scraper.ParseMarks(decodedHTML)
	if err != nil {
		failureMsg := fmt.Sprintf("Failed to parse marks data: %v", err)
		logger.Error("fetch_marks", failureMsg, err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		return false, &failureMsg
	}

	marksData := map[string]interface{}{
		"entries":    marksEntries,
		"url":        scraper.AttendanceURL,
		"fetched_at": time.Now().Format(time.RFC3339),
	}

	logger.Info("fetch_marks", "Storing marks data in cache", map[string]interface{}{
		"job_id":    job.ID,
		"user_id":   job.UserID,
		"data_type": "marks",
	})
	err = w.db.UpsertUserCache(job.UserID, "marks", marksData, 12)
	if err != nil {
		failureMsg := fmt.Sprintf("Cache storage failed: %v", err)
		logger.Error("fetch_marks", failureMsg, err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		return false, &failureMsg
	}

	logger.Info("fetch_marks", "Marks fetch process completed successfully", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
		"cached":  true,
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
	err = w.db.UpsertUser(job.UserID, tokenData.Email, userInfo)
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
