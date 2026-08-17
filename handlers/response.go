package handlers

import (
	"encoding/json"
	"log"
	"net/http"
)

// apiResponse is the standard envelope for all JSON handlers.
type apiResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// writeJSON writes a JSON response with the standard envelope and status code.
func writeJSON(w http.ResponseWriter, status int, success bool, data interface{}, errMsg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := apiResponse{
		Success: success,
	}

	if success {
		resp.Data = data
	} else {
		resp.Error = errMsg
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("JSON encode error: %v", err)
	}
}

// writeSuccess is a convenience wrapper for successful responses.
func writeSuccess(w http.ResponseWriter, status int, data interface{}) {
	writeJSON(w, status, true, data, "")
}

// writeError is a convenience wrapper for error responses.
func writeError(w http.ResponseWriter, status int, errMsg string) {
	writeJSON(w, status, false, nil, errMsg)
}

