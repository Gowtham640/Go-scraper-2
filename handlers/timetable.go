package handlers

import (
	"net/http"
	"srm-academia-scraper/jobs"
)

// TimetableHandler handles POST /timetable requests
func TimetableHandler(jobManager *jobs.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			respondFailure(w, "method_not_allowed")
			return
		}

		req, ok := decodeDataRequest(w, r, "timetable_handler")
		if !ok {
			return
		}

		handleAndEnqueueDataRequest(jobManager, "timetable_handler", "timetable", req, []string{"timetable"}, w)
		return
	}
}
