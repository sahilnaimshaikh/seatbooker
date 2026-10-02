package contract

// Reservation status values, as stored in reservations.status.
const (
	ReservationStatusConfirmed = "confirmed"
	ReservationStatusCancelled = "cancelled"
)

// Reservation is the data contract returned by the db layer for a row in
// the reservations table.
type Reservation struct {
	ID             string
	IdempotencyKey string
	ShowID         string
	UserID         string
	Seats          []string
	AmountPaise    int
	Status         string
}
