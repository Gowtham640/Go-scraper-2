package handlers

import (
	"encoding/json"
	"net/http"
	"srm-academia-scraper/jobs"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"time"
)

// CoursesHandler handles GET /courses requests
func CoursesHandler(jobManager *jobs.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		userID := r.Header.Get("X-User-Id")
		email := r.Header.Get("X-Email")
		password := r.Header.Get("X-Password")

		if r.Method != http.MethodGet {
			var req models.LoginRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				logger.Error("courses_handler", "Failed to parse request body", err, nil)
				json.NewEncoder(w).Encode(map[string]string{"response": "fail"})
				return
			}
			email = req.Account
			password = req.Password
		}
		dataType := "courses"

		if userID == "" || email == "" {
			logger.Warn("courses_handler", "Missing required fields", map[string]interface{}{
				"user_id": userID,
				"email":   email,
			})
			json.NewEncoder(w).Encode(map[string]string{"response": "fail"})
			return
		}

		logger.InfoWithUser(email, "courses_handler", "Processing courses request", nil)

		// Step 1: Check token validity first (critical for data freshness)
		tokenData, tokenErr := jobManager.GetToken(userID)

		// Step 2: Check cache freshness only if we have a valid token
		if tokenErr == nil {
			_, updatedAt, cacheErr := jobManager.GetUserCacheWithTimestamp(userID, dataType)
			if cacheErr == nil && updatedAt != nil {
				// Cache exists and is fresh (< 24 hours) AND we have valid token
				if time.Since(*updatedAt) < 24*time.Hour {
					logger.InfoWithUser(email, "courses_handler", "Fresh cache found with valid token, returning success", nil)
					json.NewEncoder(w).Encode(map[string]string{"response": "success"})
					return
				}
			}
		}

		// Step 3: No fresh cache or no valid token - need to create job
		if tokenErr != nil {
			// No token exists - create login job (highest priority for new users)
			logger.InfoWithUser(email, "courses_handler", "No token found, enqueuing login job", nil)

			jobReq := models.JobCreateRequest{
				UserID:             userID,
				JobType:            "login",
				DataType:           "auth",
				Priority:           100, // New user login
				Email:              email,
				Password:           password,
				RequestedDataTypes: []string{dataType}, // Only fetch the requested data type
			}

			err := jobManager.EnqueueJob(jobReq)
			if err != nil {
				if err.Error() == "queue_full" {
					logger.WarnWithUser(email, "courses_handler", "Queue full", nil)
					json.NewEncoder(w).Encode(map[string]string{"response": "queue_full"})
				} else {
					logger.ErrorWithUser(email, "courses_handler", "Failed to enqueue login job", err, nil)
					json.NewEncoder(w).Encode(map[string]string{"response": "fail"})
				}
				return
			}

			json.NewEncoder(w).Encode(map[string]string{"response": "success"})
			return
		}

		// Token exists - check expiry
		isExpired := time.Now().After(tokenData.ExpiryTimestamp)

		if isExpired {
			// Token expired - check if password is provided for login retry
			if password == "" {
				logger.WarnWithUser(email, "courses_handler", "Token expired but no password provided", nil)
				json.NewEncoder(w).Encode(map[string]string{"response": "fail"})
				return
			}

			// Create login job (medium priority)
			logger.InfoWithUser(email, "courses_handler", "Token expired, enqueuing login job", nil)

			jobReq := models.JobCreateRequest{
				UserID:   userID,
				JobType:  "login",
				DataType: "auth",
				Priority: 50, // Token refresh login
				Email:    email,
				Password: password,
			}

			err := jobManager.EnqueueJob(jobReq)
			if err != nil {
				if err.Error() == "queue_full" {
					logger.WarnWithUser(email, "courses_handler", "Queue full", nil)
					json.NewEncoder(w).Encode(map[string]string{"response": "queue_full"})
				} else {
					logger.ErrorWithUser(email, "courses_handler", "Failed to enqueue login job", err, nil)
					json.NewEncoder(w).Encode(map[string]string{"response": "fail"})
				}
				return
			}

			json.NewEncoder(w).Encode(map[string]string{"response": "success"})
			return
		}

		// Token is valid but no fresh cache - create fetch job
		logger.InfoWithUser(email, "courses_handler", "Token valid but no fresh cache, enqueuing fetch job", nil)

		jobReq := models.JobCreateRequest{
			UserID:   userID,
			JobType:  "fetch",
			DataType: dataType,
			Priority: 10, // Fetch job
		}

		err := jobManager.EnqueueJob(jobReq)
		if err != nil {
			logger.ErrorWithUser(email, "courses_handler", "Failed to enqueue fetch job", err, nil)
			json.NewEncoder(w).Encode(map[string]string{"response": "fail"})
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"response": "success"})
	}
}
