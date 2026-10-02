package contract

const (
	ReservationStatusConfirmed = "confirmed"
	ReservationStatusCancelled = "cancelled"
)

type Reservation struct {
	ID             string
	IdempotencyKey string
	ShowID         string
	UserID         string
	Seats          []string
	AmountPaise    int
	Status         string
}
