package handlers

import (
	"encoding/json"
	"net/http"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/storage"
)

// CoursesHandler handles GET /courses requests
func CoursesHandler(db *storage.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Get headers
		userID := r.Header.Get("X-User-Id")
		email := r.Header.Get("X-Email")
		password := r.Header.Get("X-Password")

		if userID == "" || email == "" {
			logger.Warn("courses_handler", "Missing required headers", map[string]interface{}{
				"user_id": userID,
				"email":   email,
			})
			json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
			return
		}

		logger.InfoWithUser(email, "courses_handler", "Processing courses data request", nil)

		// Fetch courses data (uses cache if available, otherwise fetches from SRM)
		coursesData, err := FetchCoursesData(db, userID, email, password)
		if err != nil {
			logger.ErrorWithUser(email, "courses_handler", "Failed to fetch courses data", err, nil)
			json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
			return
		}

		logger.InfoWithUser(email, "courses_handler", "Courses data processed successfully", map[string]interface{}{
			"count": len(coursesData.Courses),
		})
		json.NewEncoder(w).Encode(coursesData)
	}
}
