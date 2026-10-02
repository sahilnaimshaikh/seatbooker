package db

import (
	"context"

	"github.com/lib/pq"

	"github.com/paytm-hack/seatbooking/pkg/db/contract"
)

// SeatTable is bound to a single Querier at construction time — see
// ShowTable's doc comment for why.
type SeatTable struct {
	q Querier
}

func NewSeatTable(q Querier) *SeatTable {
	return &SeatTable{q: q}
}

// InsertMany creates one 'available' seat row per seatNo in a single
// statement via unnest, instead of one round trip per seat.
func (s *SeatTable) InsertMany(ctx context.Context, showID string, seatNos []string) error {
	_, err := s.q.ExecContext(ctx, insertSeatsQuery, showID, pq.Array(seatNos), contract.SeatStatusAvailable)
	return err
}

func (s *SeatTable) List(ctx context.Context, showID string) ([]contract.Seat, error) {
	rows, err := s.q.QueryContext(ctx, listSeatsQuery, showID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSeats(rows)
}

// Lock takes row locks on the given seats, visited in the order
// ORDER BY seat_no produces — the caller must pass seatNos already
// sorted, so every concurrent transaction locks seats in the same fixed
// order and multi-seat requests can never deadlock each other.
func (s *SeatTable) Lock(ctx context.Context, showID string, seatNos []string) ([]contract.Seat, error) {
	rows, err := s.q.QueryContext(ctx, lockSeatsQuery, showID, pq.Array(seatNos))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSeats(rows)
}

// Confirm flips the given seats to confirmed, but only the ones still
// available — this WHERE-guarded UPDATE is the actual no-double-sell
// guarantee, not the preceding Lock call (which only reports what the
// caller observed, not what's still true at write time without this
// guard). RowsAffected tells the caller how many of the requested seats
// actually got confirmed.
func (s *SeatTable) Confirm(ctx context.Context, showID string, seatNos []string, userID string) (int64, error) {
	res, err := s.q.ExecContext(ctx, confirmSeatsQuery,
		contract.SeatStatusConfirmed, userID, showID, pq.Array(seatNos), contract.SeatStatusAvailable)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Release flips the given seats back to available, but only the ones
// still confirmed to exactly this holder — so a release can never
// resurrect a seat already confirmed to someone else.
func (s *SeatTable) Release(ctx context.Context, showID string, seatNos []string, userID string) error {
	_, err := s.q.ExecContext(ctx, releaseSeatsQuery,
		contract.SeatStatusAvailable, showID, pq.Array(seatNos), contract.SeatStatusConfirmed, userID)
	return err
}

func (s *SeatTable) CountByStatus(ctx context.Context, showID, status string) (int, error) {
	var count int
	err := s.q.QueryRowContext(ctx, countSeatsByStatusQuery, showID, status).Scan(&count)
	return count, err
}

func scanSeats(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]contract.Seat, error) {
	var seats []contract.Seat
	for rows.Next() {
		var seat contract.Seat
		if err := rows.Scan(&seat.SeatNo, &seat.Status); err != nil {
			return nil, err
		}
		seats = append(seats, seat)
	}
	return seats, rows.Err()
}
