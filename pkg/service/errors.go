package service

// Code identifies a specific domain outcome a service call can return.
// Handlers map each Code to an HTTP status — the service layer itself
// knows nothing about HTTP.
type Code string

const (
	CodeNotFound            Code = "not_found"
	CodeSeatTaken           Code = "seat_taken"
	CodePerUserLimit        Code = "per_user_limit"
	CodeIdempotencyConflict Code = "idempotency_key_reused"
	CodeInvalidInput        Code = "invalid_input"
)

// Error is a domain-level outcome (a clean decline, not a crash). Service
// methods return *Error for every expected failure path — seat already
// taken, over limit, not found, bad input — so callers can branch on Code
// without parsing strings or depending on database error types.
type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

func newError(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}
