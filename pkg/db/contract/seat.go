package contract

const (
	SeatStatusAvailable = "available"
	SeatStatusConfirmed = "confirmed"
)

type Seat struct {
	SeatNo string
	Status string
}
