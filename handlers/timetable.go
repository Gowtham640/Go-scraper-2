package handlers

import (
	"net/http"
	"srm-academia-scraper/jobs"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"time"
)

// TimetableHandler handles POST /timetable requests
func TimetableHandler(jobManager *jobs.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			respondFailure(w, "method_not_allowed")
			return
		}

		req, ok := decodeDataRequest(w, r, "timetable_handler")
		if !ok {
			return
		}

		userID := req.UserID
		email := req.Email
		password := req.Password
		logger.InfoWithUser(email, "timetable_handler", "Processing timetable request", map[string]interface{}{
			"user_type": req.UserType,
		})

		dataType := "timetable"

		tokenData, tokenErr := jobManager.GetToken(userID)

		if tokenErr == nil {
			_, updatedAt, cacheErr := jobManager.GetUserCacheWithTimestamp(userID, dataType)
			if cacheErr == nil && updatedAt != nil && time.Since(*updatedAt) < 24*time.Hour {
				logger.InfoWithUser(email, "timetable_handler", "Fresh cache found with valid token, returning success", nil)
				respondSuccess(w)
				return
			}
		}

		if tokenErr != nil {
			logger.InfoWithUser(email, "timetable_handler", "No token found, enqueuing login job", nil)

			jobReq := models.JobCreateRequest{
				UserID:             userID,
				JobType:            "login",
				DataType:           "auth",
				Priority:           100,
				Email:              email,
				Password:           password,
				RequestedDataTypes: []string{dataType},
			}

			if err := jobManager.EnqueueJob(jobReq); err != nil {
				if err.Error() == "queue_full" {
					logger.WarnWithUser(email, "timetable_handler", "Queue full", nil)
					respondFailure(w, "queue_full")
					return
				}
				logger.ErrorWithUser(email, "timetable_handler", "Failed to enqueue login job", err, nil)
				respondFailure(w, "fail")
				return
			}

			respondSuccess(w)
			return
		}

		isExpired := time.Now().After(tokenData.ExpiryTimestamp)

		if isExpired {
			if password == "" {
				logger.WarnWithUser(email, "timetable_handler", "Token expired but no password provided", nil)
				respondFailure(w, "fail")
				return
			}

			logger.InfoWithUser(email, "timetable_handler", "Token expired, enqueuing login job", nil)

			jobReq := models.JobCreateRequest{
				UserID:   userID,
				JobType:  "login",
				DataType: "auth",
				Priority: 50,
				Email:    email,
				Password: password,
			}

			if err := jobManager.EnqueueJob(jobReq); err != nil {
				if err.Error() == "queue_full" {
					logger.WarnWithUser(email, "timetable_handler", "Queue full", nil)
					respondFailure(w, "queue_full")
					return
				}
				logger.ErrorWithUser(email, "timetable_handler", "Failed to enqueue login job", err, nil)
				respondFailure(w, "fail")
				return
			}

			respondSuccess(w)
			return
		}

		logger.InfoWithUser(email, "timetable_handler", "Token valid but no fresh cache, enqueuing fetch job", nil)

		jobReq := models.JobCreateRequest{
			UserID:   userID,
			JobType:  "fetch",
			DataType: dataType,
			Priority: 10,
		}

		if err := jobManager.EnqueueJob(jobReq); err != nil {
			logger.ErrorWithUser(email, "timetable_handler", "Failed to enqueue fetch job", err, nil)
			respondFailure(w, "fail")
			return
		}

		respondSuccess(w)
	}
}
