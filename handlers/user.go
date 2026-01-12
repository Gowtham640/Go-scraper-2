package handlers

import (
	"encoding/json"
	"net/http"
	"srm-academia-scraper/auth"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/storage"
)

// UserHandler handles GET /user requests
func UserHandler(db *storage.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Get headers
		userID := r.Header.Get("X-User-Id")
		email := r.Header.Get("X-Email")

		if userID == "" || email == "" {
			logger.Warn("user_handler", "Missing required headers", map[string]interface{}{
				"user_id": userID,
				"email":   email,
			})
			json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
			return
		}

		logger.InfoWithUser(email, "user_handler", "Processing user data request", nil)

		// Fetch user info using stored token (will update users.html and parse data)
		sessionManager := auth.NewSessionManager(db)
		_, err := sessionManager.FetchUserInfo(userID, email)
		if err != nil {
			logger.ErrorWithUser(email, "user_handler", "Failed to fetch user info", err, nil)
			json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
			return
		}

		logger.InfoWithUser(email, "user_handler", "User data processed successfully", nil)
		json.NewEncoder(w).Encode(models.SuccessResponse{Success: true})
	}
}
