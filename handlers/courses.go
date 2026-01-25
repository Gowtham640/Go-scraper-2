package handlers

import (
	"net/http"
	"srm-academia-scraper/jobs"
)

// CoursesHandler handles POST /courses requests
func CoursesHandler(jobManager *jobs.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			respondFailure(w, "method_not_allowed")
			return
		}

		req, ok := decodeDataRequest(w, r, "courses_handler")
		if !ok {
			return
		}

		handleAndEnqueueDataRequest(jobManager, "courses_handler", "courses", req, []string{"courses"}, w)
		return
	}
}
