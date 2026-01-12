package handlers

import (
	"encoding/json"
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
			json.NewEncoder(w).Encode(models.LoginResponse{
				Success: false,
				Error:   "Invalid request body",
			})
			return
		}

		logger.InfoWithUser(req.Account, "login_handler", "Processing login request", nil)

		// Initialize session manager for browser login
		sessionManager := auth.NewSessionManager(db)

		// Perform login and create user in auth.users, store token
		userID, err := sessionManager.LoginAndCreateUser(req.Account, req.Password)
		if err != nil {
			logger.ErrorWithUser(req.Account, "login_handler", "Login failed", err, nil)
			json.NewEncoder(w).Encode(models.LoginResponse{
				Success: false,
				Error:   "Authentication failed",
			})
			return
		}

		logger.InfoWithUser(req.Account, "login_handler", "Login successful", map[string]interface{}{
			"user_id": userID,
		})

		// Return success with user ID
		json.NewEncoder(w).Encode(models.LoginResponse{
			Success: true,
			UserId:  userID,
		})
	}
}
