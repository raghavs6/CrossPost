package handler

import (
	"encoding/json"
	"net/http"
)

// healthResponse defines the shape of the /health JSON response.
type healthResponse struct {
	Message string `json:"message"`
}

// HealthHandler responds with a JSON message confirming the server is running.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	resp := healthResponse{Message: "OmniPost is running"}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
