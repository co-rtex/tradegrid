package httpapi

import (
	"errors"
	"log"
	"net/http"

	"github.com/co-rtex/tradegrid/backend/internal/exchange"
)

// Routing codes from docs/api.md. The exchange never returns these; the HTTP
// layer decides them before any service call.
const (
	codeNotFound         exchange.Code = "NOT_FOUND"
	codeMethodNotAllowed exchange.Code = "METHOD_NOT_ALLOWED"
)

// errorResponse is the body of every error response (docs/api.md, "Errors").
type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    exchange.Code `json:"code"`
	Message string        `json:"message"`
	Field   string        `json:"field,omitempty"`
}

// codeStatus maps each service error code to its HTTP status. A code missing
// here is answered as 500, so a new code fails loudly in tests instead of
// reaching clients with a guessed status.
var codeStatus = map[exchange.Code]int{
	exchange.CodeInvalidQuery:         http.StatusBadRequest,
	exchange.CodeUnknownUser:          http.StatusNotFound,
	exchange.CodeMissingField:         http.StatusUnprocessableEntity,
	exchange.CodeUnknownSymbol:        http.StatusUnprocessableEntity,
	exchange.CodeInvalidSide:          http.StatusUnprocessableEntity,
	exchange.CodeUnsupportedOrderType: http.StatusUnprocessableEntity,
	exchange.CodeInvalidPrice:         http.StatusUnprocessableEntity,
	exchange.CodeInvalidQuantity:      http.StatusUnprocessableEntity,
	exchange.CodeInsufficientCash:     http.StatusUnprocessableEntity,
	exchange.CodeInsufficientShares:   http.StatusUnprocessableEntity,
	exchange.CodeLimitExceeded:        http.StatusUnprocessableEntity,
	exchange.CodeInternal:             http.StatusInternalServerError,
}

// writeError writes err as a contracted error response. Errors that are not
// an *exchange.Error, and internal failures, are logged and answered with a
// generic INTERNAL message so internal details never reach the client.
func writeError(w http.ResponseWriter, err error) {
	var e *exchange.Error
	if !errors.As(err, &e) {
		e = &exchange.Error{Code: exchange.CodeInternal, Err: err}
	}
	status, ok := codeStatus[e.Code]
	if !ok || status == http.StatusInternalServerError {
		log.Printf("httpapi: internal error: %v", err)
		writeErrorBody(w, http.StatusInternalServerError, exchange.CodeInternal, "internal error", "")
		return
	}
	writeErrorBody(w, status, e.Code, e.Err.Error(), e.Field)
}

// writeErrorBody writes one error response. An empty field is omitted.
func writeErrorBody(w http.ResponseWriter, status int, code exchange.Code, message, field string) {
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: message, Field: field}})
}
