package db

const (
	insertShowQuery = `INSERT INTO shows (name, price_paise, per_user_limit)
		VALUES ($1, $2, $3) RETURNING id`

	getShowQuery = `SELECT name, price_paise, per_user_limit
		FROM shows WHERE id = $1`

	insertSeatQuery = `INSERT INTO seats (show_id, seat_no, status)
		VALUES ($1, $2, $3)`

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
)
