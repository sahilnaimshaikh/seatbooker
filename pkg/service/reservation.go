package service

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/paytm-hack/seatbooking/pkg/db"
	"github.com/paytm-hack/seatbooking/pkg/db/contract"
)

const pgUniqueViolation = "23505"

type ReservationService struct {
	conn *sql.DB
}

func NewReservationService(conn *sql.DB) *ReservationService {
	return &ReservationService{conn: conn}
}

type Reservation struct {
	ID          string
	ShowID      string
	UserID      string
	Seats       []string
	AmountPaise int
	Status      string
}

func (r *ReservationService) Reserve(ctx context.Context, showID, userID string, requestedSeats []string, idempotencyKey string) (Reservation, error) {
	if len(requestedSeats) == 0 || idempotencyKey == "" {
		return Reservation{}, newError(CodeInvalidInput, "seats and idempotency_key are required")
	}

	seats := append([]string(nil), requestedSeats...)
	sort.Strings(seats)

	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return Reservation{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
		return Reservation{}, err
	}

	shows := db.NewShowTable(tx)
	seatTable := db.NewSeatTable(tx)
	counts := db.NewUserShowCountTable(tx)
	reservations := db.NewReservationTable(tx)

	show, err := shows.Get(ctx, showID)
	if errors.Is(err, sql.ErrNoRows) {
		return Reservation{}, newError(CodeNotFound, "show not found")
	}
	if err != nil {
		return Reservation{}, err
	}
	amountPaise := show.PricePaise * len(seats)

	if err := counts.Ensure(ctx, showID, userID); err != nil {
		return Reservation{}, err
	}
	heldCount, err := counts.Lock(ctx, showID, userID)
	if err != nil {
		return Reservation{}, err
	}
	if heldCount+len(seats) > show.PerUserLimit {
		return Reservation{}, newError(CodePerUserLimit, "reservation would exceed per-user seat limit")
	}

	reservationID, insertErr := reservations.Insert(ctx, idempotencyKey, showID, userID, seats, amountPaise, contract.ReservationStatusConfirmed)
	if insertErr != nil {
		if isUniqueViolation(insertErr) {
			tx.Rollback()
			return r.replayIdempotentRequest(ctx, idempotencyKey, seats)
		}
		return Reservation{}, insertErr
	}

	lockedSeats, err := seatTable.Lock(ctx, showID, seats)
	if err != nil {
		return Reservation{}, err
	}
	statusBySeat := make(map[string]string, len(lockedSeats))
	for _, seat := range lockedSeats {
		statusBySeat[seat.SeatNo] = seat.Status
	}
	for _, seatNo := range seats {
		status, ok := statusBySeat[seatNo]
		if !ok {
			return Reservation{}, newError(CodeNotFound, "seat "+seatNo+" does not exist on this show")
		}
		if status != contract.SeatStatusAvailable {
			return Reservation{}, newError(CodeSeatTaken, "one or more requested seats are no longer available")
		}
	}

	affected, err := seatTable.Confirm(ctx, showID, seats, userID)
	if err != nil {
		return Reservation{}, err
	}
	if affected != int64(len(seats)) {
		return Reservation{}, newError(CodeSeatTaken, "one or more requested seats are no longer available")
	}

	if err := counts.Increment(ctx, showID, userID, len(seats)); err != nil {
		return Reservation{}, err
	}

	if err := tx.Commit(); err != nil {
		return Reservation{}, err
	}

	return Reservation{
		ID:          reservationID,
		ShowID:      showID,
		UserID:      userID,
		Seats:       seats,
		AmountPaise: amountPaise,
		Status:      contract.ReservationStatusConfirmed,
	}, nil
}

func (r *ReservationService) replayIdempotentRequest(ctx context.Context, idempotencyKey string, requestedSeats []string) (Reservation, error) {
	reservations := db.NewReservationTable(r.conn)
	existing, err := reservations.GetByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		return Reservation{}, err
	}

	if !sameSeats(existing.Seats, requestedSeats) {
		return Reservation{}, newError(CodeIdempotencyConflict, "idempotency key was already used with a different set of seats")
	}

	return Reservation{
		ID:          existing.ID,
		ShowID:      existing.ShowID,
		UserID:      existing.UserID,
		Seats:       existing.Seats,
		AmountPaise: existing.AmountPaise,
		Status:      existing.Status,
	}, nil
}

func sameSeats(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgUniqueViolation
	}
	return false
}
