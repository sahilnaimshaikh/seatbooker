package handler

import "time"

type AuthConfig struct {
	JWTSecret string
	JWTExpiry time.Duration
}

type LoginRequest struct {
	UserID string `json:"user_id"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

type HealthResponse struct {
	Status string `json:"status"`
}

type CreateShowRequest struct {
	Name         string   `json:"name"`
	Seats        []string `json:"seats"`
	PricePaise   int      `json:"price_paise"`
	PerUserLimit int      `json:"per_user_limit"`
}

type ReserveRequest struct {
	Seats          []string `json:"seats"`
	IdempotencyKey string   `json:"idempotency_key"`
}

type ShowResponse struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	PricePaise   int            `json:"price_paise"`
	PerUserLimit int            `json:"per_user_limit"`
	Seats        []SeatResponse `json:"seats"`
	Available    int            `json:"available"`
	Confirmed    int            `json:"confirmed"`
	TotalSeats   int            `json:"total_seats"`
}

type SeatResponse struct {
	SeatNo string `json:"seat_no"`
	Status string `json:"status"`
}

type ReservationResponse struct {
	ReservationID string   `json:"reservation_id"`
	ShowID        string   `json:"show_id"`
	UserID        string   `json:"user_id"`
	Seats         []string `json:"seats"`
	AmountPaise   int      `json:"amount_paise"`
	Status        string   `json:"status"`
}
