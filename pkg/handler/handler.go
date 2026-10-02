package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/paytm-hack/seatbooking/pkg/constants"
	"github.com/paytm-hack/seatbooking/pkg/httpjson"
	"github.com/paytm-hack/seatbooking/pkg/reqctx"
	"github.com/paytm-hack/seatbooking/pkg/service"
)

type ShowService interface {
	CreateShow(ctx context.Context, name string, seatNos []string, pricePaise, perUserLimit int) (service.ShowSummary, error)
	GetShow(ctx context.Context, showID string) (service.ShowSummary, error)
}

type ReservationService interface {
	Reserve(ctx context.Context, showID, userID string, seats []string, idempotencyKey string) (service.Reservation, error)
	Cancel(ctx context.Context, reservationID, userID string) error
}

func Login(auth AuthConfig) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		var input LoginRequest
		if err := decodeJSON(writer, request, &input); err != nil {
			writeInvalidJSON(writer)
			return
		}
		userID := strings.TrimSpace(input.UserID)
		if userID == "" {
			httpjson.WriteError(writer, http.StatusBadRequest, "invalid_input", "user_id is required")
			return
		}
		token, err := issueUserToken(auth.JWTSecret, userID, auth.JWTExpiry)
		if err != nil {
			httpjson.WriteError(writer, http.StatusInternalServerError, "internal_error", "could not issue user token")
			return
		}
		httpjson.Write(writer, http.StatusOK, LoginResponse{Token: token})
	}
}

func CreateShow(shows ShowService) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		var input CreateShowRequest
		if err := decodeJSON(writer, request, &input); err != nil {
			writeInvalidJSON(writer)
			return
		}
		if input.PerUserLimit == 0 {
			input.PerUserLimit = constants.DefaultPerUserLimit
		}
		summary, err := shows.CreateShow(request.Context(), input.Name, input.Seats, input.PricePaise, input.PerUserLimit)
		if err != nil {
			writeServiceError(writer, err)
			return
		}
		httpjson.Write(writer, http.StatusCreated, toShowResponse(summary))
	}
}

func GetShow(shows ShowService) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		showID := mux.Vars(request)["id"]
		if _, err := uuid.Parse(showID); err != nil {
			httpjson.WriteError(writer, http.StatusBadRequest, "invalid_input", "show id must be a UUID")
			return
		}
		summary, err := shows.GetShow(request.Context(), showID)
		if err != nil {
			writeServiceError(writer, err)
			return
		}
		httpjson.Write(writer, http.StatusOK, toShowResponse(summary))
	}
}

func ReserveSeat(reservations ReservationService) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		showID := mux.Vars(request)["id"]
		if _, err := uuid.Parse(showID); err != nil {
			httpjson.WriteError(writer, http.StatusBadRequest, "invalid_input", "show id must be a UUID")
			return
		}
		var input ReserveRequest
		if err := decodeJSON(writer, request, &input); err != nil {
			writeInvalidJSON(writer)
			return
		}
		if hasDuplicateSeats(input.Seats) {
			httpjson.WriteError(writer, http.StatusBadRequest, "invalid_input", "seats must not contain duplicates")
			return
		}
		reservation, err := reservations.Reserve(request.Context(), showID, reqctx.UserID(request.Context()), input.Seats, input.IdempotencyKey)
		if err != nil {
			writeServiceError(writer, err)
			return
		}
		httpjson.Write(writer, http.StatusCreated, toReservationResponse(reservation))
	}
}

func hasDuplicateSeats(seats []string) bool {
	seen := make(map[string]struct{}, len(seats))
	for _, seat := range seats {
		if _, exists := seen[seat]; exists {
			return true
		}
		seen[seat] = struct{}{}
	}
	return false
}

func CancelReservation(reservations ReservationService) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		reservationID := mux.Vars(request)["id"]
		if _, err := uuid.Parse(reservationID); err != nil {
			httpjson.WriteError(writer, http.StatusBadRequest, "invalid_input", "reservation id must be a UUID")
			return
		}
		if err := reservations.Cancel(request.Context(), reservationID, reqctx.UserID(request.Context())); err != nil {
			writeServiceError(writer, err)
			return
		}
		httpjson.Write(writer, http.StatusOK, map[string]string{"status": "cancelled"})
	}
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, destination any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, 1<<20)
	defer request.Body.Close()

	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request must contain exactly one JSON value")
		}
		return err
	}
	return nil
}

func writeInvalidJSON(writer http.ResponseWriter) {
	httpjson.WriteError(writer, http.StatusBadRequest, "invalid_input", "request body must contain one valid JSON object with supported fields")
}

func writeServiceError(writer http.ResponseWriter, err error) {
	var serviceErr *service.Error
	if !errors.As(err, &serviceErr) {
		httpjson.WriteError(writer, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	status := http.StatusBadRequest
	switch serviceErr.Code {
	case service.CodeNotFound:
		status = http.StatusNotFound
	case service.CodeConflict, service.CodeSeatTaken, service.CodePerUserLimit, service.CodeIdempotencyConflict:
		status = http.StatusConflict
	case service.CodeInvalidInput:
		status = http.StatusBadRequest
	default:
		status = http.StatusInternalServerError
	}
	httpjson.WriteError(writer, status, string(serviceErr.Code), serviceErr.Message)
}

func toShowResponse(summary service.ShowSummary) ShowResponse {
	response := ShowResponse{
		ID:           summary.Show.ID,
		Name:         summary.Show.Name,
		PricePaise:   summary.Show.PricePaise,
		PerUserLimit: summary.Show.PerUserLimit,
		Available:    summary.Available,
		Confirmed:    summary.Confirmed,
		TotalSeats:   summary.TotalSeats,
		Seats:        make([]SeatResponse, 0, len(summary.Seats)),
	}
	for _, seat := range summary.Seats {
		response.Seats = append(response.Seats, SeatResponse{SeatNo: seat.SeatNo, Status: seat.Status})
	}
	return response
}

func toReservationResponse(reservation service.Reservation) ReservationResponse {
	return ReservationResponse{
		ReservationID: reservation.ID,
		ShowID:        reservation.ShowID,
		UserID:        reservation.UserID,
		Seats:         reservation.Seats,
		AmountPaise:   reservation.AmountPaise,
		Status:        reservation.Status,
	}
}
