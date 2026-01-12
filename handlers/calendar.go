package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"srm-academia-scraper/auth"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/scraper"
	"srm-academia-scraper/storage"
	"strings"
	"time"
)

// CalendarHandler handles GET /calendar requests
func CalendarHandler(db *storage.SupabaseClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Get headers
		userID := r.Header.Get("X-User-Id")
		email := r.Header.Get("X-Email")
		password := r.Header.Get("X-Password")

		if userID == "" || email == "" {
			logger.Warn("calendar_handler", "Missing required headers", map[string]interface{}{
				"user_id": userID,
				"email":   email,
			})
			json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
			return
		}

		logger.InfoWithUser(email, "calendar_handler", "Processing calendar request", nil)

		// Get token and check expiry status
		sessionManager := auth.NewSessionManager(db)
		cookies, isExpired, err := sessionManager.GetValidToken(userID, email)
		if err != nil {
			logger.ErrorWithUser(email, "calendar_handler", "Failed to get token", err, nil)
			json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
			return
		}

		// Fetch calendar HTML from SRM portal
		scraper.RateLimit(1 * time.Second)
		httpClient := scraper.NewHTTPClient()
		htmlContent, err := httpClient.GetWithCookies(scraper.CalendarURL, cookies)

		logger.InfoWithUser(email, "calendar_handler", fmt.Sprintf("Raw HTML response length: %d", len(htmlContent)), nil)

		// Save raw HTML for debugging
		rawFile := "calendar_raw.html"
		if writeErr := os.WriteFile(rawFile, []byte(htmlContent), 0644); writeErr != nil {
			logger.WarnWithUser(email, "calendar_handler", "Failed to save raw HTML", map[string]interface{}{"error": writeErr.Error()})
		} else {
			logger.InfoWithUser(email, "calendar_handler", fmt.Sprintf("Saved raw HTML to %s", rawFile), nil)
		}

		// If request fails with 401 AND token is expired, trigger browser login
		if err != nil && strings.Contains(err.Error(), "401") && isExpired {
			logger.InfoWithUser(email, "calendar_handler", "Token expired and 401 received, triggering browser login", nil)

			if password == "" {
				logger.ErrorWithUser(email, "calendar_handler", "Password required for browser login retry", nil, nil)
				json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
				return
			}

			// Perform browser login to refresh token
			newCookies, err := sessionManager.PerformBrowserLogin(userID, email, password)
			if err != nil {
				logger.ErrorWithUser(email, "calendar_handler", "Browser login retry failed", err, nil)
				json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
				return
			}

			// Update stored token with new cookies
			expiryDays := auth.ExtractExpiryDays(newCookies)
			err = db.UpsertToken(userID, email, newCookies, expiryDays)
			if err != nil {
				logger.ErrorWithUser(email, "calendar_handler", "Failed to update token after browser login", err, nil)
				json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
				return
			}

			// Retry the request with new cookies
			scraper.RateLimit(1 * time.Second)
			htmlContent, err = httpClient.GetWithCookies(scraper.CalendarURL, newCookies)
			if err != nil {
				logger.ErrorWithUser(email, "calendar_handler", "Failed to fetch calendar data with new token", err, nil)
				json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
				return
			}
		} else if err != nil {
			logger.ErrorWithUser(email, "calendar_handler", "Failed to fetch calendar data", err, nil)
			json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
			return
		}

		// Decode HTML entities (calendar uses HTML entity encoding, not JavaScript hex escaping)
		actualHTML := scraper.DecodeHTMLEntities(string(htmlContent))

		// Save decoded HTML for debugging
		err = os.WriteFile("calendar.html", []byte(actualHTML), 0644)
		if err != nil {
			logger.WarnWithUser(email, "calendar_handler", "Failed to save decoded HTML", map[string]interface{}{"error": err.Error()})
		} else {
			logger.InfoWithUser(email, "calendar_handler", "Saved decoded HTML to calendar.html", nil)
		}

		// Parse calendar data
		calendarData, err := scraper.ParseCalendar(actualHTML)
		if err != nil {
			logger.ErrorWithUser(email, "calendar_handler", "Failed to parse calendar", err, nil)
			json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
			return
		}

		// Clean calendar data before normalization
		scraper.CleanCalendarData(calendarData)

		// Normalize calendar data to flat structure
		normalizedCalendar := scraper.NormalizeCalendarData(calendarData)

		// Store normalized calendar data in public.calendar table with course="Default" and semester=0
		err = db.UpsertCalendar("Default", 0, normalizedCalendar)
		if err != nil {
			logger.ErrorWithUser(email, "calendar_handler", "Failed to store calendar", err, nil)
			json.NewEncoder(w).Encode(models.SuccessResponse{Success: false})
			return
		}

		logger.InfoWithUser(email, "calendar_handler", "Calendar processed and stored successfully", nil)
		json.NewEncoder(w).Encode(calendarData)
	}
}
