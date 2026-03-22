package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"srm-academia-scraper/auth"
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

		// Check global Playwright limit before proceeding
		runningCount, err := db.CountRunningLoginJobs()
		if err != nil {
			logger.ErrorWithUser(email, "login_handler", "Failed to check running jobs", err, nil)
			sendJSONResponse(w, email, models.LoginResponse{
				Success: false,
				Error:   "Service temporarily unavailable",
			})
			return
		}

		if runningCount >= 3 {
			logger.WarnWithUser(email, "login_handler", "Playwright limit reached", map[string]interface{}{
				"running_jobs": runningCount,
			})
			sendJSONResponse(w, email, models.LoginResponse{
				Success: false,
				Error:   "Service busy, try again later",
			})
			return
		}

		// Initialize session manager for browser login
		sessionManager := auth.NewSessionManager(db)

		// Perform login and create user in auth.users, store token
		userID, err := sessionManager.LoginAndCreateUser(email, req.Password)
		if err != nil {
			logger.ErrorWithUser(email, "login_handler", "Login failed", err, nil)
			sendJSONResponse(w, email, models.LoginResponse{
				Success: false,
				Error:   "Authentication failed",
			})
			return
		}

		logger.InfoWithUser(email, "login_handler", "Login successful (encrypted password upsert attempted inside session)", map[string]interface{}{
			"user_id": userID,
		})

		// Return success with user ID
		sendJSONResponse(w, email, models.LoginResponse{
			Success: true,
			UserId:  userID,
		})

		// Fetch user info asynchronously (kept around for later caching)
		go func(requestEmail string) {
			if _, err := sessionManager.FetchUserInfo(userID, requestEmail); err != nil {
				logger.WarnWithUser(requestEmail, "login_handler", "Async user info fetch failed", map[string]interface{}{
					"error": err.Error(),
				})
			}
		}(email)
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
