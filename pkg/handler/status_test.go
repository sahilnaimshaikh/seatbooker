package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/paytm-hack/seatbooking/pkg/reqctx"
	"github.com/paytm-hack/seatbooking/pkg/service"
)

type closeTrackingBody struct {
	*strings.Reader
	closed bool
}

func (body *closeTrackingBody) Close() error {
	body.closed = true
	return nil
}

func TestWriteServiceErrorMapsReservationOutcomes(t *testing.T) {
	tests := []struct {
		code       service.Code
		wantStatus int
	}{
		{code: service.CodeNotFound, wantStatus: http.StatusNotFound},
		{code: service.CodeInvalidInput, wantStatus: http.StatusBadRequest},
		{code: service.CodeConflict, wantStatus: http.StatusConflict},
		{code: service.CodeSeatTaken, wantStatus: http.StatusConflict},
		{code: service.CodePerUserLimit, wantStatus: http.StatusConflict},
		{code: service.CodeIdempotencyConflict, wantStatus: http.StatusConflict},
	}

	for _, test := range tests {
		t.Run(string(test.code), func(t *testing.T) {
			response := httptest.NewRecorder()
			writeServiceError(response, &service.Error{Code: test.code, Message: "test"})
			if response.Code != test.wantStatus {
				t.Fatalf("expected status %d, got %d", test.wantStatus, response.Code)
			}
		})
	}
}

func TestDecodeJSONClosesRequestBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	body := &closeTrackingBody{Reader: strings.NewReader(`{"user_id":"buyer"}`)}
	request.Body = body
	response := httptest.NewRecorder()
	var input LoginRequest

	if err := decodeJSON(response, request, &input); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if !body.closed {
		t.Fatal("expected request body to be closed after decoding")
	}
}

type reservationServiceStub struct {
	called bool
}

func (stub *reservationServiceStub) Reserve(context.Context, string, string, []string, string) (service.Reservation, error) {
	stub.called = true
	return service.Reservation{}, nil
}

func (*reservationServiceStub) Cancel(context.Context, string, string) error { return nil }

func TestReserveSeatRejectsDuplicateSeats(t *testing.T) {
	reservations := &reservationServiceStub{}
	request := httptest.NewRequest(http.MethodPost, "/shows/3b241101-e2bb-4255-8caf-4136c566a962/reserve", strings.NewReader(`{"seats":["A1","A1"],"idempotency_key":"key-1"}`))
	request = mux.SetURLVars(request, map[string]string{"id": "3b241101-e2bb-4255-8caf-4136c566a962"})
	request = request.WithContext(reqctx.WithUserID(request.Context(), "buyer-1"))
	response := httptest.NewRecorder()

	ReserveSeat(reservations).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
	if reservations.called {
		t.Fatal("reservation service must not be called for duplicate seats")
	}
}
