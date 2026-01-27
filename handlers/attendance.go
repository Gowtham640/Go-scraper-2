package handlers

import (
	"net/http"
	"srm-academia-scraper/jobs"
)

// AttendanceHandler handles POST /attendance requests
func AttendanceHandler(jobManager *jobs.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			respondFailure(w, "method_not_allowed")
			return
		}

		req, ok := decodeDataRequest(w, r, "attendance_handler")
		if !ok {
			return
		}

		handleAndEnqueueDataRequest(jobManager, "attendance_handler", "attendance", req, []string{"attendance"}, w)
		return
	}
}
