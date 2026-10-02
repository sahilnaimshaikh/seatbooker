package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestReservationErrorMapsLockTimeoutToConflict(t *testing.T) {
	err := reservationError(&pgconn.PgError{Code: pgLockNotAvailable})
	serviceErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected service error, got %T", err)
	}
	if serviceErr.Code != CodeConflict {
		t.Fatalf("expected code %q, got %q", CodeConflict, serviceErr.Code)
	}
}
