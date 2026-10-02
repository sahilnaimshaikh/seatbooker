package db

import "context"

// UserShowCountTable is bound to a single Querier at construction time —
// see ShowTable's doc comment for why.
type UserShowCountTable struct {
	q Querier
}

func NewUserShowCountTable(q Querier) *UserShowCountTable {
	return &UserShowCountTable{q: q}
}

// Ensure creates the (showID, userID) counter row if it doesn't already
// exist, defaulting held_count to 0. Safe to call every time before
// locking — ON CONFLICT DO NOTHING makes it a no-op on repeat calls.
func (u *UserShowCountTable) Ensure(ctx context.Context, showID, userID string) error {
	_, err := u.q.ExecContext(ctx, ensureUserShowCountQuery, showID, userID)
	return err
}

// Lock takes a row lock on this user's per-show counter and returns the
// current held_count. Locking this row before touching any seats — and
// always before, never after — is what makes a user's own concurrent
// reserve requests for the same show serialize against each other instead
// of racing past the per-user limit check.
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
