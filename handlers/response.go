package handlers

import (
	"encoding/json"
	"net/http"

	"srm-academia-scraper/logger"
	"srm-academia-scraper/models"
)

func writeActionResponse(w http.ResponseWriter, resp models.ActionResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func respondSuccess(w http.ResponseWriter) {
	writeActionResponse(w, models.ActionResponse{Success: true})
}

func respondFailure(w http.ResponseWriter, reason string) {
	writeActionResponse(w, models.ActionResponse{
		Success: false,
		Reason:  reason,
	})
}

func decodeDataRequest(w http.ResponseWriter, r *http.Request, handler string) (*models.DataRequest, bool) {
	var req models.DataRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Error(handler, "Failed to parse request body", err, nil)
		respondFailure(w, "invalid_request_body")
		return nil, false
	}

	if req.UserID == "" || req.Email == "" || req.Password == "" || req.UserType == "" {
		respondFailure(w, "missing_required_fields")
		return nil, false
	}

	return &req, true
}
