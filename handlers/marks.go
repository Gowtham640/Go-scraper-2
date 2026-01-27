package handlers

import (
	"net/http"
	"srm-academia-scraper/jobs"
)

// MarksHandler handles POST /marks requests
func MarksHandler(jobManager *jobs.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			respondFailure(w, "method_not_allowed")
			return
		}

		req, ok := decodeDataRequest(w, r, "marks_handler")
		if !ok {
			return
		}

		handleAndEnqueueDataRequest(jobManager, "marks_handler", "marks", req, []string{"marks"}, w)
		return
	}
}
