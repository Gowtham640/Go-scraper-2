package worker

import (
	"context"
	"fmt"
	"golang.org/x/time/rate"
	"os"
	"srm-academia-scraper/auth"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/scraper"
	"srm-academia-scraper/storage"
	"strings"
	"time"
)

// Global limiter shared across all worker goroutines for external HTTP calls.
// Intentionally coarse-grained: 10 requests per second total.
var globalExternalRequestLimiter = rate.NewLimiter(rate.Limit(10), 10)

const allowedCalendarFetchEmail = "gr8790@srmist.edu.in"

func waitGlobalExternalRequestSlot() {
	// Background context: this is a worker, not tied to request lifecycle.
	_ = globalExternalRequestLimiter.Wait(context.Background())
}

// Worker manages background job execution
type Worker struct {
	db             *storage.SupabaseClient
	sessionManager *auth.SessionManager
}

// NewWorker creates a new background worker
func NewWorker(db *storage.SupabaseClient) *Worker {
	return &Worker{
		db:             db,
		sessionManager: auth.NewSessionManager(db),
	}
}

// Start begins the background job processing loop
func (w *Worker) Start() {
	logger.Info("worker_start", "Starting background worker", nil)

	// 2 login workers: each delegates Playwright login execution to internal context slots.
	for i := 0; i < 2; i++ {
		workerID := i + 1
		logger.Info("worker_start", "Login worker goroutine started", map[string]interface{}{
			"log_type":  "login",
			"worker_id": workerID,
		})
		go func(id int) {
			for {
				job, err := w.db.ClaimNextLoginJob()
				if err != nil {
					logger.Error("login_worker", "Failed to claim login job", err, map[string]interface{}{
						"worker_id": id,
					})
					time.Sleep(1 * time.Second)
					continue
				}
				if job == nil {
					time.Sleep(1 * time.Second)
					continue
				}

				email, emailErr := w.db.GetUserEmail(job.UserID)
				if emailErr != nil {
					logger.Error("login_worker", "Failed to resolve email for login job", emailErr, map[string]interface{}{
						"job_id":  job.ID,
						"user_id": job.UserID,
					})
					failure := fmt.Sprintf("login job missing email: %v", emailErr)
					_ = w.db.UpdateJobStatus(job.ID, "failed", &failure)
					continue
				}

				password, passwordErr := w.db.GetUserDecryptedPassword(job.UserID)
				if passwordErr != nil {
					logger.Error("login_worker", "Failed to resolve password for login job", passwordErr, map[string]interface{}{
						"job_id":  job.ID,
						"user_id": job.UserID,
					})
					failure := fmt.Sprintf("login job missing password: %v", passwordErr)
					_ = w.db.UpdateJobStatus(job.ID, "failed", &failure)
					continue
				}

				requestedDataTypes := job.RequestedDataTypes
				if len(requestedDataTypes) == 0 {
					// Jobs are persisted in DB without RequestedDataTypes metadata.
					// Fall back to core dashboard datasets so new users always get hydrated.
					requestedDataTypes = []string{"attendance", "marks", "timetable"}
				}

				loginReq := models.WorkerLoginRequest{
					UserID:             job.UserID,
					Email:              email,
					Password:           password,
					Priority:           job.Priority,
					RequestedDataTypes: requestedDataTypes,
				}

				success, failureReason := w.processLoginRequest(loginReq)
				newStatus := "done"
				if !success {
					newStatus = "failed"
				}

				if err := w.db.UpdateJobStatus(job.ID, newStatus, failureReason); err != nil {
					logger.Error("login_worker", "Failed to update login job status", err, map[string]interface{}{
						"job_id":    job.ID,
						"worker_id": id,
						"status":    newStatus,
					})
				}
			}
		}(workerID)
	}

	// 2 non-attendance fetch workers.
	for i := 0; i < 2; i++ {
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

	// 4 attendance workers.
	for i := 0; i < 4; i++ {
		workerID := i + 1
		logger.Info("worker_start", "Attendance worker goroutine started", map[string]interface{}{
			"log_type":  "attendance",
			"worker_id": workerID,
		})
		go func() {
			for {
				w.processNextAttendanceJob()
				time.Sleep(2 * time.Second) // keep scheduling intervals
			}
		}()
	}

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

			// Count one failure per fully failed job attempt (not per retry step).
			if shouldTrackTokenFailure(job) {
				updatedFailureCount, countErr := w.db.IncrementTokenFailureCount(job.UserID, failureReason)
				if countErr != nil {
					logger.Error("worker_process", "Failed to increment token failure_count", countErr, map[string]interface{}{
						"job_id":    job.ID,
						"user_id":   job.UserID,
						"data_type": job.DataType,
					})
				} else {
					logger.Info("worker_process", "Token failure_count updated after permanent job failure", map[string]interface{}{
						"job_id":        job.ID,
						"user_id":       job.UserID,
						"data_type":     job.DataType,
						"failure_count": updatedFailureCount,
					})

					if updatedFailureCount >= 2 {
						logger.Warn("worker_process", "Failure threshold reached, triggering auto relogin", map[string]interface{}{
							"job_id":        job.ID,
							"user_id":       job.UserID,
							"data_type":     job.DataType,
							"failure_count": updatedFailureCount,
						})
						w.enqueueLoginJobForUser(job.UserID, job.DataType)
					}
				}
			}

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

// processLoginRequest processes a login request with credentials.
// It is used by DB-claimed login worker goroutines.
func (w *Worker) processLoginRequest(req models.WorkerLoginRequest) (bool, *string) {
	logger.Info("process_login_request", "Processing login request", map[string]interface{}{
		"user_id": req.UserID,
		"email":   req.Email,
	})

	requestedDataTypes := req.RequestedDataTypes
	if len(requestedDataTypes) == 0 {
		// Defensive fallback: DB-backed login jobs do not persist requested data types.
		requestedDataTypes = []string{"attendance", "marks", "timetable"}
	}

	// Execute login
	success, failureReason := w.executeLoginWithCredentials(req.UserID, req.Email, req.Password)

	if success {
		logger.Info("process_login_request", "Login successful, enqueuing requested fetch jobs", map[string]interface{}{
			"user_id":              req.UserID,
			"requested_data_types": requestedDataTypes,
		})

		// Enqueue only the requested fetch jobs after successful login
		err := w.db.EnqueueSpecificFetchJobs(req.UserID, requestedDataTypes)
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

	return success, failureReason
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

	if runningCount >= 6 {
		failureMsg := "Playwright limit reached"
		logger.Warn("execute_login_with_credentials", failureMsg, map[string]interface{}{
			"running_count": runningCount,
		})
		return false, &failureMsg
	}

	// Perform browser login
	waitGlobalExternalRequestSlot()
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

	logger.Info("execute_login_with_credentials", "password_persist: calling SaveUserEncryptedPassword after token upsert", map[string]interface{}{
		"user_id":           userID,
		"email":             email,
		"password_from_job": password != "",
	})
	if saveErr := w.db.SaveUserEncryptedPassword(userID, email, password); saveErr != nil {
		logger.Error("execute_login_with_credentials", "password_persist: SaveUserEncryptedPassword returned error", saveErr, map[string]interface{}{
			"user_id": userID,
			"email":   email,
		})
	} else {
		logger.Info("execute_login_with_credentials", "password_persist: SaveUserEncryptedPassword succeeded", map[string]interface{}{
			"user_id": userID,
			"email":   email,
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
		w.enqueueLoginJobForUser(job.UserID, job.DataType)
		return false, &failureMsg
	}

	// Execute fetch based on data type
	switch job.DataType {
	case "courses":
		return w.fetchCourses(job, tokenData)
	case "timetable":
		return w.fetchTimetable(job, tokenData)
	case "calendar":
		email, emailErr := w.db.GetUserEmail(job.UserID)
		if emailErr != nil {
			failureMsg := fmt.Sprintf("calendar fetch email lookup failed: %v", emailErr)
			return false, &failureMsg
		}
		if !strings.EqualFold(email, allowedCalendarFetchEmail) {
			failureMsg := "calendar fetch not allowed for this email"
			return false, &failureMsg
		}
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
	waitGlobalExternalRequestSlot()
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
	waitGlobalExternalRequestSlot()
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
	// Store the normalized calendar JSON directly into public.calendar.data.
	calendarPayload := normalizedCalendar

	// Store in global calendar table
	err = w.db.UpsertCalendar("Default", 0, calendarPayload)
	if err != nil {
		failureMsg := fmt.Sprintf("Calendar storage failed: %v", err)
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
	waitGlobalExternalRequestSlot()
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
		logger.Warn("fetch_attendance", "Sanitized extraction failed, falling back to decoded raw HTML", map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
			"error":   err.Error(),
		})
		decodedHTML = scraper.DecodeHTMLEntities(rawHTML)
	}

	if decodedHTML == "" {
		finalURL := extractLikelyFinalURL(rawHTML, scraper.AttendanceURL)
		isLoginLike, indicators := detectLoginLikePage(rawHTML)
		if isLoginLike {
			logger.Warn("fetch_attendance", "Detected login-like page instead of attendance data", map[string]interface{}{
				"job_id":     job.ID,
				"user_id":    job.UserID,
				"final_url":  finalURL,
				"indicators": indicators,
				"snippet":    snippet(rawHTML),
			})
			w.enqueueAttendanceLogin(job.UserID)
			failureMsg := "Login page detected while fetching attendance"
			return false, &failureMsg
		}

		failureMsg := fmt.Sprintf("Failed to extract attendance HTML | page_url=%s", finalURL)
		logger.Warn("fetch_attendance", "Final URL before failing attendance job", map[string]interface{}{
			"job_id":    job.ID,
			"user_id":   job.UserID,
			"final_url": finalURL,
		})
		logger.Error("fetch_attendance", failureMsg, err, map[string]interface{}{
			"job_id":   job.ID,
			"user_id":  job.UserID,
			"page_url": finalURL,
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
	isLoginLike, _ := detectLoginLikePage(body)
	return isLoginLike
}

func detectLoginLikePage(body string) (bool, []string) {
	lowered := strings.ToLower(body)
	indicators := make([]string, 0, 8)

	if strings.Contains(lowered, "login") {
		indicators = append(indicators, "contains:login")
	}
	if strings.Contains(lowered, "signin") || strings.Contains(lowered, "sign in") {
		indicators = append(indicators, "contains:signin")
	}
	if strings.Contains(lowered, "password") || strings.Contains(lowered, "type=\"password\"") {
		indicators = append(indicators, "contains:password_field")
	}
	if strings.Contains(lowered, "email") || strings.Contains(lowered, "type=\"email\"") {
		indicators = append(indicators, "contains:email_field")
	}
	if strings.Contains(lowered, "<iframe") {
		indicators = append(indicators, "contains:iframe")
	}
	if strings.Contains(lowered, "accounts/p/") || strings.Contains(lowered, "iamcsr") || strings.Contains(lowered, "zoho") {
		indicators = append(indicators, "contains:auth_provider_marker")
	}
	if strings.Contains(lowered, "ct_csrf_token") || strings.Contains(lowered, "_zcsr_tmp") {
		indicators = append(indicators, "contains:auth_csrf_cookie_marker")
	}
	if strings.Contains(lowered, "window.location") || strings.Contains(lowered, "location.href") {
		indicators = append(indicators, "contains:client_redirect_script")
	}

	strongSignal := (strings.Contains(lowered, "login") || strings.Contains(lowered, "signin")) &&
		(strings.Contains(lowered, "password") || strings.Contains(lowered, "type=\"password\""))
	if strongSignal {
		return true, indicators
	}

	if len(indicators) >= 3 {
		return true, indicators
	}
	return false, indicators
}

func extractLikelyFinalURL(body, fallbackURL string) string {
	lowered := strings.ToLower(body)
	markers := []string{"location.href=", "window.location=", "window.location.href=", "action="}

	for _, marker := range markers {
		idx := strings.Index(lowered, marker)
		if idx == -1 {
			continue
		}

		after := body[idx+len(marker):]
		for _, quote := range []string{"\"", "'"} {
			start := strings.Index(after, quote)
			if start == -1 {
				continue
			}
			rest := after[start+1:]
			end := strings.Index(rest, quote)
			if end == -1 {
				continue
			}

			candidate := strings.TrimSpace(rest[:end])
			if candidate == "" {
				continue
			}
			if strings.HasPrefix(strings.ToLower(candidate), "https://academia.srmist.edu.in") {
				return candidate
			}
			if strings.HasPrefix(candidate, "/") {
				return scraper.SRMBaseURL + candidate
			}
		}
	}
	return fallbackURL
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
	waitGlobalExternalRequestSlot()
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

	rawHTML := string(htmlContent)
	decodedHTML, err := scraper.ExtractSanitizedHTML(rawHTML)
	if err != nil {
		logger.Warn("fetch_marks", "Sanitized extraction failed, falling back to decoded raw HTML", map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
			"error":   err.Error(),
		})
		decodedHTML = scraper.DecodeHTMLEntities(rawHTML)
	}

	if decodedHTML == "" {
		finalURL := extractLikelyFinalURL(rawHTML, scraper.AttendanceURL)
		isLoginLike, indicators := detectLoginLikePage(rawHTML)
		if isLoginLike {
			logger.Warn("fetch_marks", "Detected login-like page instead of marks data", map[string]interface{}{
				"job_id":     job.ID,
				"user_id":    job.UserID,
				"final_url":  finalURL,
				"indicators": indicators,
				"snippet":    snippet(rawHTML),
			})
			w.enqueueAttendanceLogin(job.UserID)
			failureMsg := fmt.Sprintf("Login page detected while fetching marks | page_url=%s", finalURL)
			return false, &failureMsg
		}

		failureMsg := fmt.Sprintf("Failed to extract marks HTML | page_url=%s", finalURL)
		logger.Warn("fetch_marks", "Final URL before failing marks job", map[string]interface{}{
			"job_id":    job.ID,
			"user_id":   job.UserID,
			"final_url": finalURL,
		})
		logger.Error("fetch_marks", failureMsg, err, map[string]interface{}{
			"job_id":   job.ID,
			"user_id":  job.UserID,
			"page_url": finalURL,
		})
		return false, &failureMsg
	}

	fetchedAt := time.Now().UTC()
	if err := w.parseAndPersistAttendanceAndMarks(job, decodedHTML, fetchedAt, "fetch_marks"); err != nil {
		failureMsg := err.Error()
		return false, &failureMsg
	}

	logger.Info("fetch_marks", "Marks fetch process completed successfully", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
		"cached":  true,
	})

	return true, nil
}

func (w *Worker) fetchAttendancePageHTML(job *models.Job, tokenData *models.TokenData, logType string, saveHTML bool) (string, error) {
	logger.Info(logType, "Applying rate limit before request", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
		"delay":   "1 second",
	})
	scraper.RateLimit(1 * time.Second)

	logger.Info(logType, "Creating HTTP client", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
	})
	httpClient := scraper.NewHTTPClient()

	logger.Info(logType, "Making HTTP request to attendance endpoint", map[string]interface{}{
		"job_id":      job.ID,
		"user_id":     job.UserID,
		"url":         scraper.AttendanceURL,
		"has_cookies": tokenData != nil && tokenData.Tokens != "",
	})

	waitGlobalExternalRequestSlot()
	htmlContent, err := httpClient.GetWithCookies(scraper.AttendanceURL, tokenData.Tokens)
	if err != nil && w.isAuthFailure(err) {
		logger.Warn(logType, "Authentication failure detected, triggering login", map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
			"error":   err.Error(),
		})
		w.enqueueAttendanceLogin(job.UserID)
		return "", fmt.Errorf("authentication failed - login job enqueued")
	}

	if err != nil {
		failureMsg := fmt.Sprintf("HTTP request failed: %v", err)
		logger.Error(logType, failureMsg, err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
			"url":     scraper.AttendanceURL,
		})
		return "", fmt.Errorf("%s", failureMsg)
	}

	logger.Info(logType, "HTML content successfully fetched", map[string]interface{}{
		"job_id":       job.ID,
		"user_id":      job.UserID,
		"content_size": len(htmlContent),
		"url":          scraper.AttendanceURL,
	})

	if saveHTML {
		logger.Info(logType, "Saving HTML content to attendance.html file", map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
			"file":    "attendance.html",
		})
		err = os.WriteFile("attendance.html", htmlContent, 0644)
		if err != nil {
			logger.Error(logType, "Failed to save HTML to file", err, map[string]interface{}{
				"job_id":  job.ID,
				"user_id": job.UserID,
				"file":    "attendance.html",
			})
		} else {
			logger.Info(logType, "HTML content successfully saved to file", map[string]interface{}{
				"job_id":  job.ID,
				"user_id": job.UserID,
				"file":    "attendance.html",
				"bytes":   len(htmlContent),
			})
		}
	}

	rawHTML := string(htmlContent)
	decodedHTML, err := scraper.ExtractSanitizedHTML(rawHTML)
	if err != nil {
		logger.Warn(logType, "Sanitized extraction failed, falling back to decoded raw HTML", map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
			"error":   err.Error(),
		})
		decodedHTML = scraper.DecodeHTMLEntities(rawHTML)
	}

	if decodedHTML == "" {
		finalURL := extractLikelyFinalURL(rawHTML, scraper.AttendanceURL)
		isLoginLike, indicators := detectLoginLikePage(rawHTML)
		if isLoginLike {
			logger.Warn(logType, "Detected login-like page instead of expected data", map[string]interface{}{
				"job_id":     job.ID,
				"user_id":    job.UserID,
				"final_url":  finalURL,
				"indicators": indicators,
				"snippet":    snippet(rawHTML),
			})
			w.enqueueAttendanceLogin(job.UserID)
			return "", fmt.Errorf("login page detected while fetching attendance data")
		}

		failureMsg := fmt.Sprintf("Failed to extract attendance/marks HTML | page_url=%s", finalURL)
		logger.Warn(logType, "Final URL before failing job", map[string]interface{}{
			"job_id":    job.ID,
			"user_id":   job.UserID,
			"final_url": finalURL,
		})
		logger.Error(logType, failureMsg, err, map[string]interface{}{
			"job_id":   job.ID,
			"user_id":  job.UserID,
			"page_url": finalURL,
		})
		return "", fmt.Errorf("%s", failureMsg)
	}

	logger.Info(logType, "Sanitized HTML extracted", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
		"length":  len(decodedHTML),
	})
	return decodedHTML, nil
}

func (w *Worker) parseAndPersistAttendanceAndMarks(job *models.Job, decodedHTML string, fetchedAt time.Time, logType string) error {
	attendanceEntries, err := scraper.ParseAttendance(decodedHTML)
	if err != nil {
		failureMsg := fmt.Sprintf("Failed to parse attendance data: %v", err)
		logger.Error(logType, failureMsg, err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		return fmt.Errorf("%s", failureMsg)
	}

	logger.Info(logType, "Parsed attendance entries", map[string]interface{}{
		"job_id":      job.ID,
		"user_id":     job.UserID,
		"entry_count": len(attendanceEntries),
	})

	attendanceData := map[string]interface{}{
		"entries":    attendanceEntries,
		"url":        scraper.AttendanceURL,
		"fetched_at": fetchedAt.Format(time.RFC3339),
	}

	logger.Info(logType, "Storing attendance data in cache", map[string]interface{}{
		"job_id":    job.ID,
		"user_id":   job.UserID,
		"cache_ttl": 2,
		"data_type": "attendance",
	})
	if err = w.db.UpsertUserCache(job.UserID, "attendance", attendanceData, 2); err != nil {
		failureMsg := fmt.Sprintf("Attendance cache storage failed: %v", err)
		logger.Error(logType, failureMsg, err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		return fmt.Errorf("%s", failureMsg)
	}

	processor := newAttendanceProcessor(w.db)
	if err := processor.Process(job.ID, job.UserID, attendanceEntries, fetchedAt); err != nil {
		failureMsg := fmt.Sprintf("Attendance pipeline failed: %v", err)
		logger.Error(logType, failureMsg, err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		return fmt.Errorf("%s", failureMsg)
	}

	marksEntries, err := scraper.ParseMarks(decodedHTML)
	if err != nil {
		failureMsg := fmt.Sprintf("Failed to parse marks data: %v", err)
		logger.Error(logType, failureMsg, err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		return fmt.Errorf("%s", failureMsg)
	}

	logger.Info(logType, "Parsed marks entries", map[string]interface{}{
		"job_id":      job.ID,
		"user_id":     job.UserID,
		"entry_count": len(marksEntries),
	})

	marksData := map[string]interface{}{
		"entries":    marksEntries,
		"url":        scraper.AttendanceURL,
		"fetched_at": fetchedAt.Format(time.RFC3339),
	}

	logger.Info(logType, "Storing marks data in cache", map[string]interface{}{
		"job_id":    job.ID,
		"user_id":   job.UserID,
		"data_type": "marks",
		"cache_ttl": 12,
	})
	if err = w.db.UpsertUserCache(job.UserID, "marks", marksData, 12); err != nil {
		failureMsg := fmt.Sprintf("Marks cache storage failed: %v", err)
		logger.Error(logType, failureMsg, err, map[string]interface{}{
			"job_id":  job.ID,
			"user_id": job.UserID,
		})
		return fmt.Errorf("%s", failureMsg)
	}

	return nil
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
	waitGlobalExternalRequestSlot()
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

func (w *Worker) enqueueLoginJobForUser(userID, dataType string) {
	logger.Info("execute_fetch_job", "Enqueuing login job because token missing", map[string]interface{}{
		"user_id":   userID,
		"data_type": dataType,
	})

	email, err := w.db.GetUserEmail(userID)
	if err != nil {
		logger.Warn("execute_fetch_job", fmt.Sprintf("Could not determine user email for login fallback: %v", err), map[string]interface{}{
			"user_id": userID,
		})
		email = ""
	}

	jobReq := models.JobCreateRequest{
		UserID:             userID,
		JobType:            "login",
		DataType:           "auth",
		Priority:           100,
		Email:              email,
		Password:           "",
		RequestedDataTypes: []string{dataType},
	}

	password, passwordErr := w.db.GetUserDecryptedPassword(userID)
	if passwordErr != nil {
		logger.Warn("execute_fetch_job", "Could not load stored password for auto relogin", map[string]interface{}{
			"user_id": userID,
			"error":   passwordErr.Error(),
		})
	} else {
		jobReq.Password = password
	}

	job, _, enqueueErr := w.db.EnqueueJob(jobReq)
	if enqueueErr != nil {
		if enqueueErr.Error() == "job already exists" {
			logger.Info("execute_fetch_job", "Login job already exists", map[string]interface{}{
				"user_id":   userID,
				"data_type": dataType,
			})
			return
		}

		logger.Error("execute_fetch_job", "Failed to enqueue fallback login job", enqueueErr, map[string]interface{}{
			"user_id":   userID,
			"data_type": dataType,
		})
		return
	}

	if job == nil {
		logger.Info("execute_fetch_job", "Login job already exists or was not inserted", map[string]interface{}{
			"user_id":   userID,
			"data_type": dataType,
		})
		return
	}

	logger.Info("execute_fetch_job", "Fallback login request sent to login worker", map[string]interface{}{
		"user_id":   userID,
		"data_type": dataType,
	})
}

func shouldTrackTokenFailure(job *models.Job) bool {
	if job == nil || job.JobType != "fetch" {
		return false
	}
	return job.DataType == "attendance" || job.DataType == "marks" || job.DataType == "timetable"
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
