package handlers

import (
	"net/http"
	"srm-academia-scraper/jobs"
)

// UserHandler handles POST /user requests
func UserHandler(jobManager *jobs.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			respondFailure(w, "method_not_allowed")
			return
		}

		req, ok := decodeDataRequest(w, r, "user_handler")
		if !ok {
			return
		}

		handleAndEnqueueDataRequest(jobManager, "user_handler", "user", req, []string{"user"}, w)
		return
	}
}
