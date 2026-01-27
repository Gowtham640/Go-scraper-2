package handlers

import (
	"net/http"
	"srm-academia-scraper/jobs"
)

// CalendarHandler handles POST /calendar requests
func CalendarHandler(jobManager *jobs.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			respondFailure(w, "method_not_allowed")
			return
		}

		req, ok := decodeDataRequest(w, r, "calendar_handler")
		if !ok {
			return
		}

		handleAndEnqueueDataRequest(jobManager, "calendar_handler", "calendar", req, []string{"calendar"}, w)
		return
	}
}
