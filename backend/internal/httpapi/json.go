package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
)

// jsonContentType is the Content-Type of every JSON response (docs/api.md).
const jsonContentType = "application/json; charset=utf-8"

// writeJSON writes v as a JSON response body with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", jsonContentType)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line is already sent, so the client cannot be told.
		log.Printf("httpapi: write JSON response: %v", err)
	}
}
