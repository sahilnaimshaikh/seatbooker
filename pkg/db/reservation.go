package db

import (
	"context"

	"github.com/lib/pq"

	"github.com/paytm-hack/seatbooking/pkg/db/contract"
)

// ReservationTable is bound to a single Querier at construction time — see
// ShowTable's doc comment for why.
type ReservationTable struct {
	q Querier
}

func NewReservationTable(q Querier) *ReservationTable {
	return &ReservationTable{q: q}
}

// Insert attempts to create a reservation row. idempotency_key is UNIQUE
// at the schema level — the caller is expected to detect a unique
// violation on this call and treat it as "this key was already used",
// not to pre-check for an existing row before inserting.
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

// Cancel flips a reservation to cancelled, but only if it is still owned
// by userID and still confirmed — so a non-owner cancel or a double
// cancel both affect zero rows instead of corrupting state. Returns the
// show id and seats so the caller can release the underlying seats in the
// same transaction.
func (r *ReservationTable) Cancel(ctx context.Context, reservationID, userID string) (showID string, seats []string, err error) {
	err = r.q.QueryRowContext(ctx, cancelReservationQuery,
		contract.ReservationStatusCancelled, reservationID, userID, contract.ReservationStatusConfirmed,
	).Scan(&showID, pq.Array(&seats))
	return showID, seats, err
}
