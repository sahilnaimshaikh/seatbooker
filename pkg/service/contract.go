package service

import "github.com/paytm-hack/seatbooking/pkg/db/contract"

type ShowSummary struct {
	Show       contract.Show
	Seats      []contract.Seat
	Available  int
	Confirmed  int
	TotalSeats int
}

type Reservation struct {
	ID          string
	ShowID      string
	UserID      string
	Seats       []string
	AmountPaise int
	Status      string
}
