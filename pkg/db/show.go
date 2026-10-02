package db

import (
	"context"

	"github.com/paytm-hack/seatbooking/pkg/db/contract"
)

type ShowTable struct {
	q Querier
}

func NewShowTable(q Querier) *ShowTable {
	return &ShowTable{q: q}
}

func (s *ShowTable) Insert(ctx context.Context, name string, pricePaise, perUserLimit int) (string, error) {
	var id string
	err := s.q.QueryRowContext(ctx, insertShowQuery, name, pricePaise, perUserLimit).Scan(&id)
	return id, err
}

func (s *ShowTable) Get(ctx context.Context, showID string) (contract.Show, error) {
	var show contract.Show
	show.ID = showID
	err := s.q.QueryRowContext(ctx, getShowQuery, showID).
		Scan(&show.Name, &show.PricePaise, &show.PerUserLimit)
	return show, err
}
