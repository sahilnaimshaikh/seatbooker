package db

import (
	"context"

	"github.com/lib/pq"

	"github.com/paytm-hack/seatbooking/pkg/db/contract"
)

type SeatTable struct {
	q Querier
}

func NewSeatTable(q Querier) *SeatTable {
	return &SeatTable{q: q}
}

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

func (s *SeatTable) Lock(ctx context.Context, showID string, seatNos []string) ([]contract.Seat, error) {
	rows, err := s.q.QueryContext(ctx, lockSeatsQuery, showID, pq.Array(seatNos))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSeats(rows)
}

func (s *SeatTable) Confirm(ctx context.Context, showID string, seatNos []string, userID string) (int64, error) {
	res, err := s.q.ExecContext(ctx, confirmSeatsQuery,
		contract.SeatStatusConfirmed, userID, showID, pq.Array(seatNos), contract.SeatStatusAvailable)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

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
