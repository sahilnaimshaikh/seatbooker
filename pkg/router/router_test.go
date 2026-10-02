package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/paytm-hack/seatbooking/pkg/config"
	"github.com/paytm-hack/seatbooking/pkg/db/contract"
	"github.com/paytm-hack/seatbooking/pkg/logctx"
	"github.com/paytm-hack/seatbooking/pkg/router"
	"github.com/paytm-hack/seatbooking/pkg/service"
)

const testShowID = "3b241101-e2bb-4255-8caf-4136c566a962"

type testShows struct {
	createCalled bool
	createLimit  int
}

func (shows *testShows) CreateShow(_ context.Context, name string, seats []string, pricePaise, perUserLimit int) (service.ShowSummary, error) {
	shows.createCalled = true
	shows.createLimit = perUserLimit
	return service.ShowSummary{
		Show:      contract.Show{ID: testShowID, Name: name, PricePaise: pricePaise, PerUserLimit: perUserLimit},
		Seats:     []contract.Seat{{SeatNo: seats[0], Status: contract.SeatStatusAvailable}},
		Available: 1, TotalSeats: 1,
	}, nil
}

func (*testShows) GetShow(context.Context, string) (service.ShowSummary, error) {
	return service.ShowSummary{}, nil
}

type testReservations struct {
	reserveCalled bool
	reserveUserID string
}

func (reservations *testReservations) Reserve(_ context.Context, showID, userID string, seats []string, _ string) (service.Reservation, error) {
	reservations.reserveCalled = true
	reservations.reserveUserID = userID
	return service.Reservation{
		ID: "reservation-id", ShowID: showID, UserID: userID, Seats: seats,
		AmountPaise: 25000, Status: contract.ReservationStatusConfirmed,
	}, nil
}

func (*testReservations) Cancel(context.Context, string, string) error { return nil }

func newTestRouter(shows *testShows, reservations *testReservations) http.Handler {
	ctx, _ := logctx.New(context.Background(), "test")
	return router.New(&router.Router{
		Shows:        shows,
		Reservations: reservations,
		AppConfig: &config.Config{
			JWTSecret: "jwt-secret", JWTExpiry: time.Hour, AdminToken: "admin-secret",
		},
		Context: ctx,
	})
}

func TestCreateShowRequiresAdminAndDefaultsLimit(t *testing.T) {
	shows := &testShows{}
	httpHandler := newTestRouter(shows, &testReservations{})
	request := httptest.NewRequest(http.MethodPost, "/shows", strings.NewReader(`{"name":"show","seats":["A1"],"price_paise":25000}`))
	request.Header.Set("Authorization", "Bearer admin-secret")
	response := httptest.NewRecorder()

	httpHandler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	if response.Header().Get("X-Request-Id") == "" {
		t.Fatal("expected request ID middleware to add X-Request-Id")
	}
	if !shows.createCalled || shows.createLimit != 4 {
		t.Fatalf("expected admin creation with default limit 4, got called=%t limit=%d", shows.createCalled, shows.createLimit)
	}
	var body struct {
		Data struct {
			ID    string `json:"id"`
			Seats []struct {
				SeatNo string `json:"seat_no"`
			} `json:"seats"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.ID != testShowID || len(body.Data.Seats) != 1 || body.Data.Seats[0].SeatNo != "A1" {
		t.Fatalf("unexpected create-show response: %+v", body.Data)
	}
}

func TestReserveUsesAuthenticatedUserIdentity(t *testing.T) {
	reservations := &testReservations{}
	httpHandler := newTestRouter(&testShows{}, reservations)
	request := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"user_id":"token-user"}`))
	loginResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(loginResponse, request)
	var loginBody struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&loginBody); err != nil {
		t.Fatalf("decode login response: %v", err)
	}

	request = httptest.NewRequest(http.MethodPost, "/shows/"+testShowID+"/reserve", strings.NewReader(`{"seats":["A1"],"idempotency_key":"key-1"}`))
	request.Header.Set("Authorization", "Bearer "+loginBody.Data.Token)
	response := httptest.NewRecorder()
	httpHandler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}
	if !reservations.reserveCalled || reservations.reserveUserID != "token-user" {
		t.Fatalf("expected token subject as reservation identity, got called=%t user=%q", reservations.reserveCalled, reservations.reserveUserID)
	}
}

func TestCreateShowRejectsMissingAdminToken(t *testing.T) {
	shows := &testShows{}
	httpHandler := newTestRouter(shows, &testReservations{})
	request := httptest.NewRequest(http.MethodPost, "/shows", strings.NewReader(`{"name":"show","seats":["A1"],"price_paise":25000}`))
	response := httptest.NewRecorder()

	httpHandler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized || shows.createCalled {
		t.Fatalf("expected unauthorized without invoking service, got status=%d called=%t", response.Code, shows.createCalled)
	}
}

func TestLoginRejectsEmptyUserID(t *testing.T) {
	httpHandler := newTestRouter(&testShows{}, &testReservations{})
	request := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"user_id":" "}`))
	response := httptest.NewRecorder()
	httpHandler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
}
