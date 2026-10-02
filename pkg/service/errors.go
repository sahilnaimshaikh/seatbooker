package service

type Code string

const (
	CodeNotFound            Code = "not_found"
	CodeConflict            Code = "conflict"
	CodeSeatTaken           Code = "seat_taken"
	CodePerUserLimit        Code = "per_user_limit"
	CodeIdempotencyConflict Code = "idempotency_key_reused"
	CodeInvalidInput        Code = "invalid_input"
)

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
