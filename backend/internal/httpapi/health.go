package httpapi

import "net/http"

// healthResponse is the body of GET /health.
type healthResponse struct {
	Status string `json:"status"`
}

// handleHealth reports that the HTTP server is up and answering requests. It
// deliberately checks nothing else, so it stays fast and never fails because
// another part of the backend is unavailable.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}
