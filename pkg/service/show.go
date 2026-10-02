package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/paytm-hack/seatbooking/pkg/db"
	"github.com/paytm-hack/seatbooking/pkg/db/contract"
)

// ShowService owns the business rules around creating and reading shows.
// It opens its own transactions and constructs db-layer tables bound to
// them — callers never see a *sql.Tx or a Querier.
type ShowService struct {
	conn *sql.DB
}

func NewShowService(conn *sql.DB) *ShowService {
	return &ShowService{conn: conn}
}

type ShowSummary struct {
	Show       contract.Show
	Seats      []contract.Seat
	Available  int
	Confirmed  int
	TotalSeats int
}

// CreateShow inserts the show row and one seat row per requested seat
// number, all 'available', in a single transaction — either every seat
// exists or none do.
func (s *ShowService) CreateShow(ctx context.Context, name string, seatNos []string, pricePaise, perUserLimit int) (ShowSummary, error) {
	if name == "" || len(seatNos) == 0 || pricePaise < 0 {
		return ShowSummary{}, newError(CodeInvalidInput, "name, seats, and non-negative price_paise are required")
	}

	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return ShowSummary{}, err
	}
	defer tx.Rollback()

	shows := db.NewShowTable(tx)
	seats := db.NewSeatTable(tx)

	showID, err := shows.Insert(ctx, name, pricePaise, perUserLimit)
	if err != nil {
		return ShowSummary{}, err
	}

	if err := seats.InsertMany(ctx, showID, seatNos); err != nil {
		return ShowSummary{}, err
	}

	if err := tx.Commit(); err != nil {
		return ShowSummary{}, err
	}

	summary := ShowSummary{
		Show: contract.Show{
			ID:           showID,
			Name:         name,
			PricePaise:   pricePaise,
			PerUserLimit: perUserLimit,
		},
		Available:  len(seatNos),
		TotalSeats: len(seatNos),
	}
	for _, seatNo := range seatNos {
		summary.Seats = append(summary.Seats, contract.Seat{SeatNo: seatNo, Status: contract.SeatStatusAvailable})
	}
	return summary, nil
}

// GetShow returns every seat's current status plus the reconciliation
// counts (available + confirmed == total_seats must hold here).
func (s *ShowService) GetShow(ctx context.Context, showID string) (ShowSummary, error) {
	shows := db.NewShowTable(s.conn)
	seats := db.NewSeatTable(s.conn)

	show, err := shows.Get(ctx, showID)
	if errors.Is(err, sql.ErrNoRows) {
		return ShowSummary{}, newError(CodeNotFound, "show not found")
	}
	if err != nil {
		return ShowSummary{}, err
	}

	seatList, err := seats.List(ctx, showID)
	if err != nil {
		return ShowSummary{}, err
	}

	summary := ShowSummary{Show: show, Seats: seatList}
	for _, seat := range seatList {
		summary.TotalSeats++
		switch seat.Status {
		case contract.SeatStatusAvailable:
			summary.Available++
		case contract.SeatStatusConfirmed:
			summary.Confirmed++
		}
	}
	return summary, nil
}
