package handlers

import (
	"net/http"
	"srm-academia-scraper/jobs"
)

const allowedCalendarRequestEmail = "gr8790@srmist.edu.in"

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
		if req.Email != allowedCalendarRequestEmail {
			w.WriteHeader(http.StatusForbidden)
			respondFailure(w, "calendar_email_not_allowed")
			return
		}

		handleAndEnqueueDataRequest(jobManager, "calendar_handler", "calendar", req, []string{"calendar"}, w)
		return
	}
}
