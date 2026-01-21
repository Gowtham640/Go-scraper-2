package handlers

import (
	"encoding/json"
	"net/http"
	"srm-academia-scraper/jobs"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"time"
)

// MarksHandler handles GET /marks requests
func MarksHandler(jobManager *jobs.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		userID := r.Header.Get("X-User-Id")
		dataType := "marks"

		if userID == "" {
			logger.Warn("marks_handler", "Missing user id header", nil)
			json.NewEncoder(w).Encode(map[string]string{"response": "fail", "reason": "missing_user_id"})
			return
		}

		if r.Method == http.MethodGet {
			logger.Info("marks_handler", "GET marks cache requested", map[string]interface{}{
				"user_id": userID,
			})

			data, err := jobManager.GetUserCache(userID, dataType)
			if err != nil {
				logger.Warn("marks_handler", "Cached marks not found, enqueuing fetch", map[string]interface{}{
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
					logger.Error("marks_handler", "Failed to enqueue marks fetch job", enqueueErr, map[string]interface{}{
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
			logger.Warn("marks_handler", "Method not allowed", map[string]interface{}{
				"user_id": userID,
				"method":  r.Method,
			})
			w.WriteHeader(http.StatusMethodNotAllowed)
			json.NewEncoder(w).Encode(map[string]string{"response": "fail", "reason": "method_not_allowed"})
			return
		}

		var req models.LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			logger.Error("marks_handler", "Failed to parse request body", err, map[string]interface{}{
				"user_id": userID,
			})
			json.NewEncoder(w).Encode(map[string]string{"response": "fail"})
			return
		}

		email := req.Account
		password := req.Password

		if email == "" {
			logger.Warn("marks_handler", "Missing email in request", map[string]interface{}{
				"user_id": userID,
			})
			json.NewEncoder(w).Encode(map[string]string{"response": "fail", "reason": "missing_email"})
			return
		}

		logger.InfoWithUser(email, "marks_handler", "Processing marks request", nil)

		tokenData, tokenErr := jobManager.GetToken(userID)

		if tokenErr == nil {
			_, updatedAt, cacheErr := jobManager.GetUserCacheWithTimestamp(userID, dataType)
			if cacheErr == nil && updatedAt != nil {
				if time.Since(*updatedAt) < 24*time.Hour {
					logger.InfoWithUser(email, "marks_handler", "Fresh cache found with valid token, returning success", nil)
					json.NewEncoder(w).Encode(map[string]string{"response": "success"})
					return
				}
			}
		}

		if tokenErr != nil {
			logger.InfoWithUser(email, "marks_handler", "No token found, enqueuing login job", nil)

			jobReq := models.JobCreateRequest{
				UserID:             userID,
				JobType:            "login",
				DataType:           "auth",
				Priority:           100,
				Email:              email,
				Password:           password,
				RequestedDataTypes: []string{dataType},
			}

			err := jobManager.EnqueueJob(jobReq)
			if err != nil {
				if err.Error() == "queue_full" {
					logger.WarnWithUser(email, "marks_handler", "Queue full", nil)
					json.NewEncoder(w).Encode(map[string]string{"response": "queue_full"})
				} else {
					logger.ErrorWithUser(email, "marks_handler", "Failed to enqueue login job", err, nil)
					json.NewEncoder(w).Encode(map[string]string{"response": "fail"})
				}
				return
			}

			json.NewEncoder(w).Encode(map[string]string{"response": "success"})
			return
		}

		isExpired := time.Now().After(tokenData.ExpiryTimestamp)

		if isExpired {
			if password == "" {
				logger.WarnWithUser(email, "marks_handler", "Token expired but no password provided", nil)
				json.NewEncoder(w).Encode(map[string]string{"response": "fail"})
				return
			}

			logger.InfoWithUser(email, "marks_handler", "Token expired, enqueuing login job", nil)

			jobReq := models.JobCreateRequest{
				UserID:   userID,
				JobType:  "login",
				DataType: "auth",
				Priority: 50,
				Email:    email,
				Password: password,
			}

			err := jobManager.EnqueueJob(jobReq)
			if err != nil {
				if err.Error() == "queue_full" {
					logger.WarnWithUser(email, "marks_handler", "Queue full", nil)
					json.NewEncoder(w).Encode(map[string]string{"response": "queue_full"})
				} else {
					logger.ErrorWithUser(email, "marks_handler", "Failed to enqueue login job", err, nil)
					json.NewEncoder(w).Encode(map[string]string{"response": "fail"})
				}
				return
			}

			json.NewEncoder(w).Encode(map[string]string{"response": "success"})
			return
		}

		logger.InfoWithUser(email, "marks_handler", "Token valid but no fresh cache, enqueuing fetch job", nil)

		jobReq := models.JobCreateRequest{
			UserID:   userID,
			JobType:  "fetch",
			DataType: dataType,
			Priority: 10,
		}

		err := jobManager.EnqueueJob(jobReq)
		if err != nil {
			logger.ErrorWithUser(email, "marks_handler", "Failed to enqueue fetch job", err, nil)
			json.NewEncoder(w).Encode(map[string]string{"response": "fail"})
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"response": "success"})
	}
}
