package handlers

import (
	"encoding/json"
	"net/http"
	"srm-academia-scraper/jobs"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"time"
)

// AttendanceHandler handles GET /attendance requests
func AttendanceHandler(jobManager *jobs.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		userID := r.Header.Get("X-User-Id")
		dataType := "attendance"

		if userID == "" {
			logger.Warn("attendance_handler", "Missing user id header", nil)
			json.NewEncoder(w).Encode(map[string]string{"response": "fail", "reason": "missing_user_id"})
			return
		}

		if r.Method == http.MethodGet {
			logger.Info("attendance_handler", "GET attendance cache requested", map[string]interface{}{
				"user_id": userID,
			})

			data, err := jobManager.GetUserCache(userID, dataType)
			if err != nil {
				logger.Warn("attendance_handler", "Cached attendance not found, enqueuing fetch", map[string]interface{}{
					"user_id": userID,
					"error":   err.Error(),
				})

				jobReq := models.JobCreateRequest{
					UserID:   userID,
					JobType:  "fetch",
					DataType: dataType,
					Priority: 10,
				}
				if enqueueErr := jobManager.EnqueueJob(jobReq); enqueueErr != nil {
					logger.Error("attendance_handler", "Failed to enqueue attendance fetch job", enqueueErr, map[string]interface{}{
						"user_id":   userID,
						"data_type": dataType,
					})
					json.NewEncoder(w).Encode(map[string]string{"response": "fail", "reason": "enqueue_failed"})
					return
				}

				json.NewEncoder(w).Encode(map[string]string{"response": "queued"})
				return
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"response": "success",
				"data":     data,
			})
			return
		}

		if r.Method != http.MethodPost {
			logger.Warn("attendance_handler", "Method not allowed", map[string]interface{}{
				"user_id": userID,
				"method":  r.Method,
			})
			w.WriteHeader(http.StatusMethodNotAllowed)
			json.NewEncoder(w).Encode(map[string]string{"response": "fail", "reason": "method_not_allowed"})
			return
		}

		// Extract credentials from request body
		var req models.LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			logger.Error("attendance_handler", "Failed to parse request body", err, map[string]interface{}{
				"user_id": userID,
			})
			json.NewEncoder(w).Encode(map[string]string{"response": "fail"})
			return
		}

		email := req.Account
		password := req.Password

		if email == "" {
			logger.Warn("attendance_handler", "Missing email in request", map[string]interface{}{
				"user_id": userID,
			})
			json.NewEncoder(w).Encode(map[string]string{"response": "fail", "reason": "missing_email"})
			return
		}

		logger.InfoWithUser(email, "attendance_handler", "Processing attendance request", nil)

		// Step 1: Check token validity first (critical for data freshness)
		tokenData, tokenErr := jobManager.GetToken(userID)

		// Step 2: Check cache freshness only if we have a valid token
		if tokenErr == nil {
			_, updatedAt, cacheErr := jobManager.GetUserCacheWithTimestamp(userID, dataType)
			if cacheErr == nil && updatedAt != nil {
				// Cache exists and is fresh (< 24 hours) AND we have valid token
				if time.Since(*updatedAt) < 24*time.Hour {
					logger.InfoWithUser(email, "attendance_handler", "Fresh cache found with valid token, returning success", nil)
					json.NewEncoder(w).Encode(map[string]string{"response": "success"})
					return
				}
			}
		}

		// Step 3: No fresh cache or no valid token - need to create job
		if tokenErr != nil {
			// No token exists - create login job (highest priority for new users)
			logger.InfoWithUser(email, "attendance_handler", "No token found, enqueuing login job", nil)

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
					logger.WarnWithUser(email, "attendance_handler", "Queue full", nil)
					json.NewEncoder(w).Encode(map[string]string{"response": "queue_full"})
				} else {
					logger.ErrorWithUser(email, "attendance_handler", "Failed to enqueue login job", err, nil)
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
				logger.WarnWithUser(email, "attendance_handler", "Token expired but no password provided", nil)
				json.NewEncoder(w).Encode(map[string]string{"response": "fail"})
				return
			}

			// Create login job (medium priority)
			logger.InfoWithUser(email, "attendance_handler", "Token expired, enqueuing login job", nil)

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
					logger.WarnWithUser(email, "attendance_handler", "Queue full", nil)
					json.NewEncoder(w).Encode(map[string]string{"response": "queue_full"})
				} else {
					logger.ErrorWithUser(email, "attendance_handler", "Failed to enqueue login job", err, nil)
					json.NewEncoder(w).Encode(map[string]string{"response": "fail"})
				}
				return
			}

			json.NewEncoder(w).Encode(map[string]string{"response": "success"})
			return
		}

		// Token is valid but no fresh cache - create fetch job
		logger.InfoWithUser(email, "attendance_handler", "Token valid but no fresh cache, enqueuing fetch job", nil)

		jobReq := models.JobCreateRequest{
			UserID:   userID,
			JobType:  "fetch",
			DataType: dataType,
			Priority: 10, // Fetch job
		}

		err := jobManager.EnqueueJob(jobReq)
		if err != nil {
			logger.ErrorWithUser(email, "attendance_handler", "Failed to enqueue fetch job", err, nil)
			json.NewEncoder(w).Encode(map[string]string{"response": "fail"})
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"response": "success"})
	}
}
