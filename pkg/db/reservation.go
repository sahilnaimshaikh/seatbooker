package db

import (
	"context"

	"github.com/lib/pq"

	"github.com/paytm-hack/seatbooking/pkg/db/contract"
)

type ReservationTable struct {
	q Querier
}

func NewReservationTable(q Querier) *ReservationTable {
	return &ReservationTable{q: q}
}

func (r *ReservationTable) Insert(ctx context.Context, idempotencyKey, showID, userID string, seats []string, amountPaise int, status string) (string, error) {
	var id string
	err := r.q.QueryRowContext(ctx, insertReservationQuery,
		idempotencyKey, showID, userID, pq.Array(seats), amountPaise, status,
	).Scan(&id)
	return id, err
}

func (r *ReservationTable) GetByIdempotencyKey(ctx context.Context, idempotencyKey string) (contract.Reservation, error) {
	var res contract.Reservation
	res.IdempotencyKey = idempotencyKey
	err := r.q.QueryRowContext(ctx, getReservationByIdempotencyKeyQuery, idempotencyKey).
		Scan(&res.ID, &res.ShowID, &res.UserID, pq.Array(&res.Seats), &res.AmountPaise, &res.Status)
	return res, err
}

func (r *ReservationTable) GetConfirmedShowIDForOwner(ctx context.Context, reservationID, userID string) (string, error) {
	var showID string
	err := r.q.QueryRowContext(ctx, getConfirmedReservationShowIDQuery,
		reservationID, userID, contract.ReservationStatusConfirmed,
	).Scan(&showID)
	return showID, err
}

func (r *ReservationTable) Cancel(ctx context.Context, reservationID, userID string) (showID string, seats []string, err error) {
	err = r.q.QueryRowContext(ctx, cancelReservationQuery,
		contract.ReservationStatusCancelled, reservationID, userID, contract.ReservationStatusConfirmed,
	).Scan(&showID, pq.Array(&seats))
	return showID, seats, err
}
