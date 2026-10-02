package contract

// Seat status values, as stored in seats.status.
const (
	SeatStatusAvailable = "available"
	SeatStatusConfirmed = "confirmed"
)

// Seat is the data contract returned by the db layer for a row in the
// seats table.
type Seat struct {
	SeatNo string
	Status string
}
