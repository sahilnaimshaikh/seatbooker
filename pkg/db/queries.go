package db

const (
	insertShowQuery = `INSERT INTO shows (name, price_paise, per_user_limit)
		VALUES ($1, $2, $3) RETURNING id`

	getShowQuery = `SELECT name, price_paise, per_user_limit
		FROM shows WHERE id = $1`

	listShowSeatAvailabilityQuery = `SELECT shows.id, count(seats.seat_no) FILTER (WHERE seats.status = $1)
		FROM shows LEFT JOIN seats ON seats.show_id = shows.id
		GROUP BY shows.id ORDER BY shows.id`

	insertSeatsQuery = `INSERT INTO seats (show_id, seat_no, status)
		SELECT $1, unnest($2::text[]), $3`

	listSeatsQuery = `SELECT seat_no, status FROM seats
		WHERE show_id = $1 ORDER BY seat_no`

	lockSeatsQuery = `SELECT seat_no, status FROM seats
		WHERE show_id = $1 AND seat_no = ANY($2)
		ORDER BY seat_no
		FOR UPDATE`

	confirmSeatsQuery = `UPDATE seats SET status = $1, held_by = $2
		WHERE show_id = $3 AND seat_no = ANY($4) AND status = $5`

	releaseSeatsQuery = `UPDATE seats SET status = $1, held_by = NULL
		WHERE show_id = $2 AND seat_no = ANY($3) AND status = $4 AND held_by = $5`

	countSeatsByStatusQuery = `SELECT count(*) FROM seats
		WHERE show_id = $1 AND status = $2`

	insertReservationQuery = `INSERT INTO reservations
		(idempotency_key, show_id, user_id, seats, amount_paise, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`

	getReservationByIdempotencyKeyQuery = `SELECT id, show_id, user_id, seats, amount_paise, status
		FROM reservations WHERE idempotency_key = $1`

	getConfirmedReservationShowIDQuery = `SELECT show_id FROM reservations
		WHERE id = $1 AND user_id = $2 AND status = $3`

	cancelReservationQuery = `UPDATE reservations SET status = $1
		WHERE id = $2 AND user_id = $3 AND status = $4
		RETURNING show_id, seats`

	ensureUserShowCountQuery = `INSERT INTO user_show_counts (show_id, user_id, held_count)
		VALUES ($1, $2, 0)
		ON CONFLICT (show_id, user_id) DO NOTHING`

	lockUserShowCountQuery = `SELECT held_count FROM user_show_counts
		WHERE show_id = $1 AND user_id = $2
		FOR UPDATE`

	incrementUserShowCountQuery = `UPDATE user_show_counts SET held_count = held_count + $1
		WHERE show_id = $2 AND user_id = $3`

	decrementUserShowCountQuery = `UPDATE user_show_counts SET held_count = held_count - $1
		WHERE show_id = $2 AND user_id = $3`
)
