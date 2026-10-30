package exchange

import "fmt"

// Code is a stable error code from docs/api.md. The HTTP layer maps each code
// to its status without matching message text.
type Code string

// Codes the service can return; the full list lives in docs/api.md.
const (
	CodeMissingField         Code = "MISSING_FIELD"
	CodeUnknownSymbol        Code = "UNKNOWN_SYMBOL"
	CodeInvalidSide          Code = "INVALID_SIDE"
	CodeUnsupportedOrderType Code = "UNSUPPORTED_ORDER_TYPE"
	CodeInvalidPrice         Code = "INVALID_PRICE"
	CodeInvalidQuantity      Code = "INVALID_QUANTITY"
	CodeInvalidQuery         Code = "INVALID_QUERY"
	CodeUnknownUser          Code = "UNKNOWN_USER"
	CodeInsufficientCash     Code = "INSUFFICIENT_CASH"
	CodeInsufficientShares   Code = "INSUFFICIENT_SHARES"
	CodeLimitExceeded        Code = "LIMIT_EXCEEDED"
	CodeInternal             Code = "INTERNAL"
)

// Error is a rejected request or an internal failure. Field names the
// offending request field or query parameter, when there is one.
type Error struct {
	Code  Code
	Field string
	Err   error
}

func (e *Error) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s (%s): %v", e.Code, e.Field, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

func newError(code Code, field string, err error) *Error {
	return &Error{Code: code, Field: field, Err: err}
}
