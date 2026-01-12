package handlers

import (
	"encoding/json"
	"fmt"
	"srm-academia-scraper/auth"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/scraper"
	"srm-academia-scraper/storage"
	"strings"
	"time"
)

// FetchCoursesData fetches course data for a user, using cache if available or fetching from SRM if not
func FetchCoursesData(db *storage.SupabaseClient, userID, email, password string) (*models.CoursesData, error) {
	logger.InfoWithUser(email, "fetch_courses_data", "Fetching courses data", nil)

	// First check if courses data exists in cache
	if cachedData, err := db.GetUserCache(userID, "courses"); err == nil {
		logger.InfoWithUser(email, "fetch_courses_data", "Found cached courses data", nil)

		// Parse the cached JSON data
		var coursesData models.CoursesData
		if jsonData, ok := cachedData.(string); ok {
			if err := json.Unmarshal([]byte(jsonData), &coursesData); err == nil {
				return &coursesData, nil
			}
			logger.WarnWithUser(email, "fetch_courses_data", "Failed to parse cached courses data", map[string]interface{}{"error": err.Error()})
		}
	} else {
		logger.InfoWithUser(email, "fetch_courses_data", "No cached courses data found", nil)
	}

	// Cache miss - fetch from SRM portal
	logger.InfoWithUser(email, "fetch_courses_data", "Fetching courses from SRM portal", nil)

	// Get token and check expiry status
	sessionManager := auth.NewSessionManager(db)
	cookies, isExpired, err := sessionManager.GetValidToken(userID, email)
	if err != nil {
		logger.ErrorWithUser(email, "fetch_courses_data", "Failed to get token", err, nil)
		return nil, err
	}

	// Fetch HTML from SRM portal
	scraper.RateLimit(1 * time.Second)
	httpClient := scraper.NewHTTPClient()
	htmlContent, err := httpClient.GetWithCookies(scraper.TimeTableURL, cookies)

	// If request fails with 401 AND token is expired, trigger browser login
	if err != nil && strings.Contains(err.Error(), "401") && isExpired {
		logger.InfoWithUser(email, "fetch_courses_data", "Token expired and 401 received, triggering browser login", nil)

		if password == "" {
			logger.ErrorWithUser(email, "fetch_courses_data", "Password required for browser login retry", nil, nil)
			return nil, fmt.Errorf("password required for browser login retry")
		}

		// Perform browser login to refresh token
		newCookies, err := sessionManager.PerformBrowserLogin(userID, email, password)
		if err != nil {
			logger.ErrorWithUser(email, "fetch_courses_data", "Browser login retry failed", err, nil)
			return nil, err
		}

		// Update stored token with new cookies
		expiryDays := auth.ExtractExpiryDays(newCookies)
		err = db.UpsertToken(userID, email, newCookies, expiryDays)
		if err != nil {
			logger.ErrorWithUser(email, "fetch_courses_data", "Failed to update token after browser login", err, nil)
			return nil, err
		}

		// Retry the request with new cookies
		scraper.RateLimit(1 * time.Second)
		htmlContent, err = httpClient.GetWithCookies(scraper.TimeTableURL, newCookies)
		if err != nil {
			logger.ErrorWithUser(email, "fetch_courses_data", "Failed to fetch courses data with new token", err, nil)
			return nil, err
		}
	} else if err != nil {
		logger.ErrorWithUser(email, "fetch_courses_data", "Failed to fetch courses data", err, nil)
		return nil, err
	}

	// Extract HTML from pageSanitizer.sanitize() if present (courses uses JavaScript hex escaping like user info)
	actualHTML, err := scraper.ExtractHTMLFromSanitizer(string(htmlContent), "courses.html")
	if err != nil {
		logger.ErrorWithUser(email, "fetch_courses_data", "Failed to extract HTML from sanitizer", err, nil)
		return nil, err
	}

	// Parse user info first to get registration number
	userInfo, err := scraper.ParseUserInfo(actualHTML)
	if err != nil {
		logger.ErrorWithUser(email, "fetch_courses_data", "Failed to parse user info", err, nil)
		return nil, err
	}

	// Parse courses
	coursesData, err := scraper.ParseCourses(string(htmlContent), userInfo.RegNumber)
	if err != nil {
		logger.ErrorWithUser(email, "fetch_courses_data", "Failed to parse courses", err, nil)
		return nil, err
	}

	// Clean courses data before storage (remove unnecessary backslashes)
	scraper.CleanCoursesData(coursesData)

	// Cache courses data (expires in 24 hours) - don't fail if caching fails
	err = db.UpsertUserCache(userID, "courses", coursesData, 24)
	if err != nil {
		logger.WarnWithUser(email, "fetch_courses_data", "Failed to cache courses data, continuing", map[string]interface{}{"error": err.Error()})
	} else {
		logger.InfoWithUser(email, "fetch_courses_data", "Courses data cached successfully", nil)
	}

	logger.InfoWithUser(email, "fetch_courses_data", "Courses data fetched successfully", map[string]interface{}{
		"count": len(coursesData.Courses),
	})

	return coursesData, nil
}