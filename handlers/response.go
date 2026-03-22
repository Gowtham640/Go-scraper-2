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
		logger.Warn(handler, "data_request missing required fields", map[string]interface{}{
			"user_id_present":   req.UserID != "",
			"email_present":     req.Email != "",
			"password_present":  req.Password != "",
			"user_type_present": req.UserType != "",
		})
		respondFailure(w, "missing_required_fields")
		return nil, false
	}

	if req.UserType != "new" && req.UserType != "old" {
		logger.Warn(handler, "data_request invalid user_type", map[string]interface{}{
			"user_type": req.UserType,
		})
		respondFailure(w, "invalid_user_type")
		return nil, false
	}

	logger.Info(handler, "data_request decoded: password present from client", map[string]interface{}{
		"user_id":   req.UserID,
		"email":     req.Email,
		"user_type": req.UserType,
	})

	return &req, true
}
