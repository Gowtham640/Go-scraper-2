package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"srm-academia-scraper/auth"
	"srm-academia-scraper/jobs"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/scraper"
	"srm-academia-scraper/storage"
	"strings"
	"time"
)

// FetchCoursesData fetches course data for a user, using cache if available or fetching from SRM if not
func FetchCoursesData(db *storage.SupabaseClient, userID, email, password string) (*models.CoursesData, error) {
	logger.InfoWithUser(email, "fetch_courses_data", "Fetching courses data", nil)

	// First check if courses data exists in cache
	if cachedData, err := db.GetUserCache(userID, "courses"); err == nil {
		logger.InfoWithUser(email, "fetch_courses_data", "Found cached courses data", nil)

		// Parse the cached JSON data
		var coursesData models.CoursesData
		if jsonData, ok := cachedData.(string); ok {
			if err := json.Unmarshal([]byte(jsonData), &coursesData); err == nil {
				return &coursesData, nil
			}
			logger.WarnWithUser(email, "fetch_courses_data", "Failed to parse cached courses data", map[string]interface{}{"error": err.Error()})
		}
	} else {
		logger.InfoWithUser(email, "fetch_courses_data", "No cached courses data found", nil)
	}

	// Cache miss - fetch from SRM portal
	logger.InfoWithUser(email, "fetch_courses_data", "Fetching courses from SRM portal", nil)

	// Get token and check expiry status
	sessionManager := auth.NewSessionManager(db)
	cookies, isExpired, err := sessionManager.GetValidToken(userID, email)
	if err != nil {
		logger.ErrorWithUser(email, "fetch_courses_data", "Failed to get token", err, nil)
		return nil, err
	}

	// Fetch HTML from SRM portal
	scraper.RateLimit(1 * time.Second)
	httpClient := scraper.NewHTTPClient()
	htmlContent, err := httpClient.GetWithCookies(scraper.TimeTableURL, cookies)

	// ALWAYS save the fetched HTML content to tt.html for debugging
	if htmlContent != nil {
		err := os.WriteFile("tt.html", htmlContent, 0644)
		if err != nil {
			logger.WarnWithUser(email, "fetch_courses_data", "Failed to save HTML content to tt.html", map[string]interface{}{"error": err.Error()})
		} else {
			logger.InfoWithUser(email, "fetch_courses_data", "HTML content saved to tt.html", map[string]interface{}{"length": len(htmlContent)})
		}
	}

	// If request fails with 401 AND token is expired, trigger browser login
	if err != nil && strings.Contains(err.Error(), "401") && isExpired {
		logger.InfoWithUser(email, "fetch_courses_data", "Token expired and 401 received, triggering browser login", nil)

		if password == "" {
			logger.ErrorWithUser(email, "fetch_courses_data", "Password required for browser login retry", nil, nil)
			return nil, fmt.Errorf("password required for browser login retry")
		}

		// Perform browser login to refresh token
		newCookies, err := sessionManager.PerformBrowserLogin(userID, email, password)
		if err != nil {
			logger.ErrorWithUser(email, "fetch_courses_data", "Browser login retry failed", err, nil)
			return nil, err
		}

		// Update stored token with new cookies
		expiryDays := auth.ExtractExpiryDays(newCookies)
		err = db.UpsertToken(userID, email, newCookies, expiryDays)
		if err != nil {
			logger.ErrorWithUser(email, "fetch_courses_data", "Failed to update token after browser login", err, nil)
			return nil, err
		}

		// Retry the request with new cookies
		scraper.RateLimit(1 * time.Second)
		htmlContent, err = httpClient.GetWithCookies(scraper.TimeTableURL, newCookies)
		if err != nil {
			logger.ErrorWithUser(email, "fetch_courses_data", "Failed to fetch courses data with new token", err, nil)
			return nil, err
		}

		// ALWAYS save the fetched HTML content to tt.html for debugging (browser login retry)
		if htmlContent != nil {
			err := os.WriteFile("tt.html", htmlContent, 0644)
			if err != nil {
				logger.WarnWithUser(email, "fetch_courses_data", "Failed to save HTML content to tt.html (retry)", map[string]interface{}{"error": err.Error()})
			} else {
				logger.InfoWithUser(email, "fetch_courses_data", "HTML content saved to tt.html (retry)", map[string]interface{}{"length": len(htmlContent)})
			}
		}
	} else if err != nil {
		logger.ErrorWithUser(email, "fetch_courses_data", "Failed to fetch courses data", err, nil)
		return nil, err
	}

	// Extract HTML from pageSanitizer.sanitize() if present (courses uses JavaScript hex escaping like user info)
	actualHTML, err := scraper.ExtractHTMLFromSanitizer(string(htmlContent), "courses.html")
	if err != nil {
		logger.ErrorWithUser(email, "fetch_courses_data", "Failed to extract HTML from sanitizer", err, nil)
		return nil, err
	}

	// Parse user info first to get registration number
	userInfo, err := scraper.ParseUserInfo(actualHTML)
	if err != nil {
		logger.ErrorWithUser(email, "fetch_courses_data", "Failed to parse user info", err, nil)
		return nil, err
	}

	// Parse courses
	coursesData, err := scraper.ParseCourses(string(htmlContent), userInfo.RegNumber)
	if err != nil {
		logger.ErrorWithUser(email, "fetch_courses_data", "Failed to parse courses", err, nil)
		return nil, err
	}

	// Clean courses data before storage (remove unnecessary backslashes)
	scraper.CleanCoursesData(coursesData)

	// Cache courses data (expires in 24 hours) - don't fail if caching fails
	err = db.UpsertUserCache(userID, "courses", coursesData, 24)
	if err != nil {
		logger.WarnWithUser(email, "fetch_courses_data", "Failed to cache courses data, continuing", map[string]interface{}{"error": err.Error()})
	} else {
		logger.InfoWithUser(email, "fetch_courses_data", "Courses data cached successfully", nil)
	}

	logger.InfoWithUser(email, "fetch_courses_data", "Courses data fetched successfully", map[string]interface{}{
		"count": len(coursesData.Courses),
	})

	return coursesData, nil
}

func handleAndEnqueueDataRequest(jobManager *jobs.Manager, handlerName, dataType string, req *models.DataRequest, initialRequested []string, w http.ResponseWriter) {
	email := req.Email
	userID := req.UserID

	logger.InfoWithUser(email, handlerName, fmt.Sprintf("Processing %s request", dataType), map[string]interface{}{
		"user_type": req.UserType,
	})

	_, updatedAt, expiresAt, cacheErr := jobManager.GetUserCacheWithTimestamp(userID, dataType)
	cacheFresh := false
	if expiresAt != nil && time.Now().Before(*expiresAt) {
		cacheFresh = true
	}

	cacheInfo := map[string]interface{}{
		"data_type":   dataType,
		"has_cache":   cacheErr == nil,
		"cache_fresh": cacheFresh,
	}

	if updatedAt != nil {
		cacheInfo["updated_at"] = updatedAt
	}
	if expiresAt != nil {
		cacheInfo["expires_at"] = expiresAt
	}
	if cacheErr != nil {
		cacheInfo["cache_error"] = cacheErr.Error()
	}

	logger.InfoWithUser(email, handlerName, "Cache metadata", cacheInfo)

	// Data routes always include password; persist it even when only a fetch job runs.
	logger.InfoWithUser(email, handlerName, "password_persist: invoking encrypted upsert (password from JSON body)", map[string]interface{}{
		"user_id": userID,
	})
	if err := jobManager.SaveUserEncryptedPassword(userID, email, req.Password); err != nil {
		logger.ErrorWithUser(email, handlerName, "password_persist: encrypted upsert failed", err, map[string]interface{}{
			"user_id": userID,
			"step":    "SaveUserEncryptedPassword",
		})
	} else {
		logger.InfoWithUser(email, handlerName, "password_persist: encrypted upsert finished without error", map[string]interface{}{
			"user_id": userID,
		})
	}

	tokenData, tokenErr := jobManager.GetToken(userID)
	now := time.Now()

	var jobReq models.JobCreateRequest
	if tokenErr != nil {
		logger.InfoWithUser(email, handlerName, "No token found, enqueuing login job", nil)
		jobReq = models.JobCreateRequest{
			UserID:             userID,
			JobType:            "login",
			DataType:           "auth",
			Priority:           100,
			Email:              email,
			Password:           req.Password,
			RequestedDataTypes: initialRequested,
		}
	} else if now.After(tokenData.ExpiryTimestamp) {
		if req.Password == "" {
			logger.WarnWithUser(email, handlerName, "Token expired but no password provided", nil)
			respondFailure(w, "fail")
			return
		}

		logger.InfoWithUser(email, handlerName, "Token expired, enqueuing login job", nil)
		// Ensure the worker knows which data to refresh after token renewal.
		jobReq = models.JobCreateRequest{
			UserID:             userID,
			JobType:            "login",
			DataType:           "auth",
			Priority:           50,
			Email:              email,
			Password:           req.Password,
			RequestedDataTypes: []string{dataType},
		}
	} else {
		missingCritical := auth.GetMissingCriticalCookies(tokenData.Tokens)
		if len(missingCritical) > 0 {
			if req.Password == "" {
				logger.WarnWithUser(email, handlerName, "Missing critical tokens but no password provided", map[string]interface{}{
					"missing_tokens": missingCritical,
				})
				respondFailure(w, "fail")
				return
			}

			logger.WarnWithUser(email, handlerName, "Critical tokens missing, enqueuing login job", map[string]interface{}{
				"missing_tokens": missingCritical,
			})
			jobReq = models.JobCreateRequest{
				UserID:             userID,
				JobType:            "login",
				DataType:           "auth",
				Priority:           100,
				Email:              email,
				Password:           req.Password,
				RequestedDataTypes: initialRequested,
			}
		} else {
			logger.InfoWithUser(email, handlerName, "Token valid, enqueuing fetch job", nil)
			jobReq = models.JobCreateRequest{
				UserID:   userID,
				JobType:  "fetch",
				DataType: dataType,
				Priority: 10,
			}
		}
	}

	if err := jobManager.EnqueueJob(jobReq); err != nil {
		if errors.Is(err, storage.ErrQueueFull) {
			logger.WarnWithUser(email, handlerName, "Queue full", map[string]interface{}{
				"user_type": req.UserType,
			})
			if req.UserType == "old" {
				respondFailure(w, "queue_full")
				return
			}
			respondSuccess(w)
			return
		}
		logger.ErrorWithUser(email, handlerName, "Failed to enqueue job", err, map[string]interface{}{
			"job_type":  jobReq.JobType,
			"data_type": jobReq.DataType,
		})
		respondFailure(w, "fail")
		return
	}

	if req.UserType == "old" {
		logger.InfoWithUser(email, handlerName, "Old request accepted; refresh queued", nil)
	}
	respondSuccess(w)
}
