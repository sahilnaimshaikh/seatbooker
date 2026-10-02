package db

import "context"

type UserShowCountTable struct {
	q Querier
}

func NewUserShowCountTable(q Querier) *UserShowCountTable {
	return &UserShowCountTable{q: q}
}

func (u *UserShowCountTable) Ensure(ctx context.Context, showID, userID string) error {
	_, err := u.q.ExecContext(ctx, ensureUserShowCountQuery, showID, userID)
	return err
}

func (u *UserShowCountTable) Lock(ctx context.Context, showID, userID string) (int, error) {
	var heldCount int
	err := u.q.QueryRowContext(ctx, lockUserShowCountQuery, showID, userID).Scan(&heldCount)
	return heldCount, err
}

func (u *UserShowCountTable) Increment(ctx context.Context, showID, userID string, by int) error {
	_, err := u.q.ExecContext(ctx, incrementUserShowCountQuery, by, showID, userID)
	return err
}

func (u *UserShowCountTable) Decrement(ctx context.Context, showID, userID string, by int) error {
	_, err := u.q.ExecContext(ctx, decrementUserShowCountQuery, by, showID, userID)
	return err
}
