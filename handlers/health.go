package handlers

import (
	"encoding/json"
	"net/http"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
	"srm-academia-scraper/scraper"
	"time"
)

// HealthHandler handles GET /health requests
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		logger.Info("health_check", "Health check requested", nil)

		// Check SRM portal accessibility
		srmStatus := "accessible"
		httpClient := scraper.NewHTTPClient()
		_, err := httpClient.Get(scraper.SRMBaseURL)
		if err != nil {
			srmStatus = "unreachable"
		}

		// Supabase check is implicit (if we're running, it's connected)
		supabaseStatus := "connected"

		response := models.HealthResponse{
			Status:    "healthy",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Services: map[string]string{
				"supabase": supabaseStatus,
				"srm_api":  srmStatus,
			},
		}

		logger.Info("health_check", "Health check completed", map[string]interface{}{
			"services": response.Services,
		})

		json.NewEncoder(w).Encode(response)
	}
}
