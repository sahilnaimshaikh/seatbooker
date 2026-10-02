package main

type apiEnvelope struct {
	Data struct {
		ID            string `json:"id"`
		ReservationID string `json:"reservation_id"`
		Available     int    `json:"available"`
		Confirmed     int    `json:"confirmed"`
		TotalSeats    int    `json:"total_seats"`
	} `json:"data"`
	Error *apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type createShowRequest struct {
	Name         string   `json:"name"`
	Seats        []string `json:"seats"`
	PricePaise   int      `json:"price_paise"`
	PerUserLimit int      `json:"per_user_limit"`
}

type reserveRequest struct {
	Seats          []string `json:"seats"`
	IdempotencyKey string   `json:"idempotency_key"`
}

type reservationAttempt struct {
	status        int
	reservationID string
	apiError      *apiError
	err           error
}

type scenarioResults struct {
	requests          int
	newConfirmations  int
	idempotentReplays int
	http5xx           int
	transportErrors   int
	declinedByReason  map[string]int
}

type showState struct {
	available int
	confirmed int
	total     int
}
