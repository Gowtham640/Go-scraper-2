package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/storage"
)

// LoginHandler handles POST /login requests
func LoginHandler(db *storage.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Parse request body
		var req models.LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			logger.Error("login_handler", "Failed to parse request body", err, nil)
			sendJSONResponse(w, "", models.LoginResponse{
				Success: false,
				Error:   "Invalid request body",
			})
			return
		}

		email := req.Account
		if email == "" {
			email = req.Email
		}
		if email == "" || req.Password == "" {
			logger.Error("login_handler", "Missing credentials in request (password or email empty)", nil, map[string]interface{}{
				"email_provided":    email != "",
				"password_provided": req.Password != "",
			})
			sendJSONResponse(w, email, models.LoginResponse{
				Success: false,
				Error:   "Email and password are required",
			})
			return
		}

		logger.InfoWithUser(email, "login_handler", "Processing login request", nil)

		// Soft gate based on pending login queue depth in public.jobs
		pendingCount, err := db.CountPendingLoginJobs()
		if err != nil {
			logger.ErrorWithUser(email, "login_handler", "Failed to check pending login jobs", err, nil)
			sendJSONResponse(w, email, models.LoginResponse{
				Success: false,
				Error:   "Service temporarily unavailable",
			})
			return
		}

		if pendingCount > 10 {
			logger.WarnWithUser(email, "login_handler", "Pending login queue limit reached", map[string]interface{}{
				"pending_jobs": pendingCount,
			})
			sendJSONResponse(w, email, models.LoginResponse{
				Success: false,
				Error:   "Service busy, try again later",
			})
			return
		}

		// Ensure we have a user ID for enqueued login jobs.
		userID, err := db.GetUserByEmail(email)
		if err != nil {
			// User may not exist yet in auth.users; create and continue.
			userID, err = db.CreateAuthUser(email, req.Password)
			if err != nil {
				logger.ErrorWithUser(email, "login_handler", "Failed to resolve user for login job enqueue", err, nil)
				sendJSONResponse(w, email, models.LoginResponse{
					Success: false,
					Error:   "Authentication failed",
				})
				return
			}
		}

		// Persist encrypted portal password so login workers can execute queued login jobs.
		if saveErr := db.SaveUserEncryptedPassword(userID, email, req.Password); saveErr != nil {
			logger.ErrorWithUser(email, "login_handler", "Failed to persist encrypted password before enqueue", saveErr, map[string]interface{}{
				"user_id": userID,
			})
			sendJSONResponse(w, email, models.LoginResponse{
				Success: false,
				Error:   "Service temporarily unavailable",
			})
			return
		}

		// Enqueue direct login request into public.jobs (single source of truth).
		jobReq := models.JobCreateRequest{
			UserID:             userID,
			JobType:            "login",
			DataType:           "auth",
			Priority:           100,
			JobSource:          models.JobSourceExternal,
			Email:              email,
			Password:           req.Password,
			RequestedDataTypes: []string{},
		}
		_, _, err = db.EnqueueJob(jobReq)
		if err != nil {
			if errors.Is(err, storage.ErrLoginRateLimited) {
				logger.WarnWithUser(email, "login_handler", "Login rejected: recent successful auth (cooldown)", map[string]interface{}{
					"user_id": userID,
				})
				sendJSONResponse(w, email, models.LoginResponse{
					Success: false,
					Error:   "Please wait five minutes after your last successful login before retrying.",
				})
				return
			}
			if err.Error() != "job already exists" {
				logger.ErrorWithUser(email, "login_handler", "Failed to enqueue direct login job", err, map[string]interface{}{
					"user_id": userID,
				})
				sendJSONResponse(w, email, models.LoginResponse{
					Success: false,
					Error:   "Service temporarily unavailable",
				})
				return
			}
		}

		logger.InfoWithUser(email, "login_handler", "Login job accepted and queued", map[string]interface{}{
			"user_id": userID,
		})

		// Return success with user ID (worker will process login from public.jobs).
		sendJSONResponse(w, email, models.LoginResponse{
			Success: true,
			UserId:  userID,
		})
	}
}

func sendJSONResponse(w http.ResponseWriter, email string, resp models.LoginResponse) {
	responseBytes, err := json.Marshal(resp)
	if err != nil {
		if email != "" {
			logger.ErrorWithUser(email, "login_handler", "Failed to marshal response for logging", err, nil)
		} else {
			logger.Error("login_handler", "Failed to marshal response for logging", err, nil)
		}
	} else {
		message := fmt.Sprintf("RESPONSE BODY: %s", string(responseBytes))
		if email != "" {
			logger.InfoWithUser(email, "login_handler", message, nil)
		} else {
			logger.Info("login_handler", message, nil)
		}
	}
	json.NewEncoder(w).Encode(resp)
}
