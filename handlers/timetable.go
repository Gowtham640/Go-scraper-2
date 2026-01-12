package handlers

import (
	"encoding/json"
	"net/http"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/scraper"
	"srm-academia-scraper/storage"
)

// TimetableHandler handles GET /timetable requests
func TimetableHandler(db *storage.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Get headers
		userID := r.Header.Get("X-User-Id")
		email := r.Header.Get("X-Email")
		password := r.Header.Get("X-Password")

		if userID == "" || email == "" {
			logger.Warn("timetable_handler", "Missing required headers", map[string]interface{}{
				"user_id": userID,
				"email":   email,
			})
			json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
			return
		}

		logger.InfoWithUser(email, "timetable_handler", "Processing timetable request", nil)


		// Get user batch - first try from users table, default to batch 2
		userBatch := "2" // Default batch
		if batchFromDB, err := db.GetUserBatch(userID); err == nil && batchFromDB != "" {
			userBatch = batchFromDB
			logger.InfoWithUser(email, "timetable_handler", "Using batch from users table", map[string]interface{}{"batch": userBatch})
		} else {
			logger.InfoWithUser(email, "timetable_handler", "Using default batch 2", map[string]interface{}{"batch": userBatch})
		}

		// Fetch courses data (uses cache if available, otherwise fetches from SRM)
		coursesData, err := FetchCoursesData(db, userID, email, password)
		if err != nil {
			logger.ErrorWithUser(email, "timetable_handler", "Failed to fetch courses data", err, nil)
			json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
			return
		}

		// Generate timetable
		timetableData, err := scraper.GenerateTimetable(coursesData.Courses, userBatch)
		if err != nil {
			logger.ErrorWithUser(email, "timetable_handler", "Failed to generate timetable", err, nil)
			json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
			return
		}

		// Set registration number and batch
		timetableData.RegNumber = coursesData.RegNumber
		timetableData.Batch = userBatch

		// Store timetable data in user_cache (expires in 7 days)
		err = db.UpsertUserCache(userID, "timetable", timetableData, 7*24)
		if err != nil {
			logger.ErrorWithUser(email, "timetable_handler", "Failed to store timetable", err, nil)
			json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
			return
		}

		logger.InfoWithUser(email, "timetable_handler", "Timetable processed and stored successfully", nil)
		json.NewEncoder(w).Encode(timetableData)
	}
}
