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
const pgLockNotAvailable = "55P03"

type ReservationService struct {
	conn *sql.DB
}

func NewReservationService(conn *sql.DB) *ReservationService {
	return &ReservationService{conn: conn}
}

func (r *ReservationService) Reserve(ctx context.Context, showID, userID string, requestedSeats []string, idempotencyKey string) (reservation Reservation, resultErr error) {
	defer func() {
		resultErr = reservationError(resultErr)
	}()

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

	existing, err := reservations.GetByIdempotencyKey(ctx, idempotencyKey)
	if err == nil {
		return replayReservation(existing, showID, userID, seats)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Reservation{}, err
	}

	show, err := shows.Get(ctx, showID)
	if errors.Is(err, sql.ErrNoRows) {
		return Reservation{}, newError(CodeNotFound, "show not found")
	}
	if err != nil {
		return Reservation{}, err
	}
	amountPaise := show.PricePaise * len(seats)
	reservationID, insertErr := reservations.Insert(ctx, idempotencyKey, showID, userID, seats, amountPaise, contract.ReservationStatusConfirmed)
	if insertErr != nil {
		if isUniqueViolation(insertErr) {
			tx.Rollback()
			return r.replayIdempotentRequest(ctx, idempotencyKey, showID, userID, seats)
		}
		return Reservation{}, insertErr
	}

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

func (r *ReservationService) Cancel(ctx context.Context, reservationID, userID string) error {
	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	reservations := db.NewReservationTable(tx)
	seatTable := db.NewSeatTable(tx)
	counts := db.NewUserShowCountTable(tx)

	showID, err := reservations.GetConfirmedShowIDForOwner(ctx, reservationID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return newError(CodeNotFound, "reservation not found")
	}
	if err != nil {
		return err
	}
	if _, err := counts.Lock(ctx, showID, userID); err != nil {
		return err
	}

	cancelledShowID, seats, err := reservations.Cancel(ctx, reservationID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return newError(CodeNotFound, "reservation not found")
	}
	if err != nil {
		return err
	}
	sort.Strings(seats)
	if _, err := seatTable.Lock(ctx, cancelledShowID, seats); err != nil {
		return err
	}

	if err := seatTable.Release(ctx, cancelledShowID, seats, userID); err != nil {
		return err
	}

	if err := counts.Decrement(ctx, cancelledShowID, userID, len(seats)); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *ReservationService) replayIdempotentRequest(ctx context.Context, idempotencyKey, showID, userID string, requestedSeats []string) (Reservation, error) {
	reservations := db.NewReservationTable(r.conn)
	existing, err := reservations.GetByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		return Reservation{}, err
	}
	return replayReservation(existing, showID, userID, requestedSeats)
}

func replayReservation(existing contract.Reservation, showID, userID string, requestedSeats []string) (Reservation, error) {
	if existing.ShowID != showID || existing.UserID != userID || !sameSeats(existing.Seats, requestedSeats) {
		return Reservation{}, newError(CodeIdempotencyConflict, "idempotency key was already used with a different request")
	}

	return Reservation{
		ID:          existing.ID,
		ShowID:      existing.ShowID,
		UserID:      existing.UserID,
		Seats:       existing.Seats,
		AmountPaise: existing.AmountPaise,
		Status:      existing.Status,
		Replayed:    true,
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

func reservationError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgLockNotAvailable {
		return newError(CodeConflict, "reservation could not acquire the required locks")
	}
	return err
}
