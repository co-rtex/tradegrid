package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/co-rtex/tradegrid/backend/internal/exchange"
	"github.com/co-rtex/tradegrid/backend/internal/executions"
)

// POST /api/v1/orders (docs/api.md). The handler decodes the request body
// into the six client-owned fields and passes only those to the exchange,
// which validates, reserves, matches, and settles the order in one
// serialized step. The handler never touches the order book directly, and
// it never retries: a POST is answered exactly once.

// maxRequestBody is the largest accepted request body (docs/api.md,
// "Numeric Bounds").
const maxRequestBody = 4096

// Request-decoding codes from docs/api.md. The exchange never returns these;
// the HTTP layer decides them before any service call.
const (
	codeMalformedJSON        exchange.Code = "MALFORMED_JSON"
	codeUnknownField         exchange.Code = "UNKNOWN_FIELD"
	codeBodyTooLarge         exchange.Code = "BODY_TOO_LARGE"
	codeUnsupportedMediaType exchange.Code = "UNSUPPORTED_MEDIA_TYPE"
)

// Request body fields, in the order docs/api.md validates them.
const (
	fieldUserID   = "userId"
	fieldSymbol   = "symbol"
	fieldSide     = "side"
	fieldType     = "type"
	fieldPrice    = "price"
	fieldQuantity = "quantity"
)

var (
	stringFields = []string{fieldUserID, fieldSymbol, fieldSide, fieldType}
	intFields    = []string{fieldPrice, fieldQuantity}
)

// decodeError is a request rejected before it reaches the exchange.
type decodeError struct {
	status  int
	code    exchange.Code
	field   string
	message string
}

func malformed(field, format string, args ...any) *decodeError {
	return &decodeError{status: http.StatusBadRequest, code: codeMalformedJSON, field: field,
		message: fmt.Sprintf(format, args...)}
}

// submitBody is a decoded request body. present records which fields the
// body contained, so an absent price or quantity is MISSING_FIELD rather
// than a zero that would be reported as INVALID_PRICE.
type submitBody struct {
	req     exchange.SubmitRequest
	present map[string]bool
}

// handleSubmitOrder answers POST /api/v1/orders. An accepted order is 201
// Created whether it rested, partly filled, or fully filled.
func handleSubmitOrder(svc *exchange.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, derr := decodeSubmit(w, r)
		if derr != nil {
			writeErrorBody(w, derr.status, derr.code, derr.message, derr.field)
			return
		}
		if err := checkPresent(svc, body); err != nil {
			writeError(w, err)
			return
		}
		res, err := svc.Submit(body.req)
		if err != nil {
			writeError(w, err)
			return
		}
		// An order that only rested has no fills; the contract requires [].
		if res.Executions == nil {
			res.Executions = []executions.Record{}
		}
		writeJSON(w, http.StatusCreated, res)
	}
}

// decodeSubmit checks the content type, reads at most maxRequestBody bytes,
// and decodes exactly one JSON object holding only the six request fields.
// Failures are reported in docs/api.md's order: content type (415), size
// (413), then the body itself (400: MALFORMED_JSON before UNKNOWN_FIELD).
func decodeSubmit(w http.ResponseWriter, r *http.Request) (submitBody, *decodeError) {
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		return submitBody{}, &decodeError{status: http.StatusUnsupportedMediaType, code: codeUnsupportedMediaType,
			message: "Content-Type must be application/json"}
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return submitBody{}, &decodeError{status: http.StatusRequestEntityTooLarge, code: codeBodyTooLarge,
				message: fmt.Sprintf("request body must be at most %d bytes", maxRequestBody)}
		}
		return submitBody{}, malformed("", "could not read request body")
	}

	fields, keys, derr := decodeObject(raw)
	if derr != nil {
		return submitBody{}, derr
	}
	body := submitBody{present: make(map[string]bool, len(fields))}
	for _, name := range stringFields {
		v, ok := fields[name]
		if !ok {
			continue
		}
		s, derr := decodeString(name, v)
		if derr != nil {
			return submitBody{}, derr
		}
		body.present[name] = true
		switch name {
		case fieldUserID:
			body.req.UserID = s
		case fieldSymbol:
			body.req.Symbol = s
		case fieldSide:
			body.req.Side = s
		case fieldType:
			body.req.Type = s
		}
	}
	for _, name := range intFields {
		v, ok := fields[name]
		if !ok {
			continue
		}
		n, derr := decodeInt(name, v)
		if derr != nil {
			return submitBody{}, derr
		}
		body.present[name] = true
		if name == fieldPrice {
			body.req.Price = n
		} else {
			body.req.Quantity = n
		}
	}
	// Every known field has been decoded, so whatever is left is unknown,
	// including server-owned fields such as id or status. Report the first
	// one in body order so the error is deterministic.
	for _, k := range keys {
		if !isRequestField(k) {
			return submitBody{}, &decodeError{status: http.StatusBadRequest, code: codeUnknownField, field: k,
				message: fmt.Sprintf("unknown field %q", k)}
		}
	}
	return body, nil
}

// decodeObject parses raw as exactly one JSON object and returns its
// members and their keys in body order. It rejects anything else: an empty
// body, a non-object value, a duplicated key, or data after the object.
//
// Keys are matched exactly. encoding/json's struct decoding matches keys
// case-insensitively, which would accept "UserId" as userId; the contract's
// field names are exact, so the body is read member by member instead.
func decodeObject(raw []byte) (map[string]json.RawMessage, []string, *decodeError) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err == io.EOF {
		return nil, nil, malformed("", "request body is empty; it must be a JSON object")
	}
	if err != nil {
		return nil, nil, malformed("", "request body is not valid JSON")
	}
	if tok != json.Delim('{') {
		return nil, nil, malformed("", "request body must be a JSON object")
	}

	fields := make(map[string]json.RawMessage)
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, malformed("", "request body is not valid JSON")
		}
		// Inside an object the decoder only yields string keys.
		key := tok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, nil, malformed("", "request body is not valid JSON")
		}
		if _, dup := fields[key]; dup {
			return nil, nil, malformed(key, "field %q appears more than once", key)
		}
		fields[key] = v
		keys = append(keys, key)
	}
	if _, err := dec.Token(); err != nil { // the closing '}'
		return nil, nil, malformed("", "request body is not valid JSON")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, nil, malformed("", "request body must contain one JSON object and nothing after it")
	}
	return fields, keys, nil
}

// decodeString decodes a string field. null and every non-string JSON type
// are the wrong type.
func decodeString(name string, v json.RawMessage) (string, *decodeError) {
	var s string
	if v[0] != '"' || json.Unmarshal(v, &s) != nil {
		return "", malformed(name, "%s must be a string", name)
	}
	return s, nil
}

// decodeInt decodes an integer field. encoding/json rejects strings,
// fractions, exponents, and integers outside int64 when the target is an
// int64; null would silently decode as 0, so it is rejected first.
func decodeInt(name string, v json.RawMessage) (int64, *decodeError) {
	var n int64
	if bytes.Equal(v, []byte("null")) || json.Unmarshal(v, &n) != nil {
		return 0, malformed(name, "%s must be an integer", name)
	}
	return n, nil
}

func isRequestField(name string) bool {
	switch name {
	case fieldUserID, fieldSymbol, fieldSide, fieldType, fieldPrice, fieldQuantity:
		return true
	}
	return false
}

// isJSONContentType reports whether a request Content-Type is
// application/json. A charset parameter, if present, must be UTF-8, the only
// encoding JSON allows.
func isJSONContentType(header string) bool {
	mt, params, err := mime.ParseMediaType(header)
	if err != nil || mt != "application/json" {
		return false
	}
	cs, ok := params["charset"]
	return !ok || strings.EqualFold(cs, "utf-8")
}

// checkPresent reports a missing price or quantity, which the exchange
// cannot see because SubmitRequest holds plain integers. It keeps
// docs/api.md's error order: an unknown user (404) first, then
// MISSING_FIELD in field order, so a missing price never hides an unknown
// user or a missing userId. When both numbers are present the exchange does
// all of these checks itself.
func checkPresent(svc *exchange.Service, body submitBody) error {
	if body.present[fieldPrice] && body.present[fieldQuantity] {
		return nil
	}
	req := body.req
	if req.UserID != "" && !hasAccount(svc, req.UserID) {
		return &exchange.Error{Code: exchange.CodeUnknownUser, Field: fieldUserID,
			Err: fmt.Errorf("unknown user %q", req.UserID)}
	}
	for _, f := range []struct{ name, value string }{
		{fieldUserID, req.UserID}, {fieldSymbol, req.Symbol}, {fieldSide, req.Side}, {fieldType, req.Type},
	} {
		if f.value == "" {
			return &exchange.Error{Code: exchange.CodeMissingField, Field: f.name, Err: fmt.Errorf("%s is required", f.name)}
		}
	}
	for _, name := range intFields {
		if !body.present[name] {
			return &exchange.Error{Code: exchange.CodeMissingField, Field: name, Err: fmt.Errorf("%s is required", name)}
		}
	}
	return nil
}

func hasAccount(svc *exchange.Service, userID string) bool {
	for _, a := range svc.Accounts() {
		if a.UserID == userID {
			return true
		}
	}
	return false
}
