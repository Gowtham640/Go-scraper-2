package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"srm-academia-scraper/config"
	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
)

// HealthHandler handles GET /health requests
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		logger.Info("health_check", "Health check requested", nil)

		nodeStatus := "unreachable"
		supabaseStatus := "connected"

		ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
		defer cancel()

		authPort := "3001"
		if config.AppConfig != nil && config.AppConfig.AuthServicePort != "" {
			authPort = config.AppConfig.AuthServicePort
		}
		authPingURL := fmt.Sprintf("http://127.0.0.1:%s", authPort)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, authPingURL, nil)
		if err == nil {
			client := &http.Client{
				Timeout: 500 * time.Millisecond,
			}
			if resp, err := client.Do(req); err == nil {
				nodeStatus = "reachable"
				resp.Body.Close()
			} else {
				logger.Error("health_check", "Auth browser ping failed", err, nil)
			}
		}

		response := models.HealthResponse{
			Status:    "healthy",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Services: map[string]string{
				"supabase":  supabaseStatus,
				"node_auth": nodeStatus,
			},
		}

		logger.Info("health_check", "Health check completed", map[string]interface{}{
			"services": response.Services,
		})

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}
