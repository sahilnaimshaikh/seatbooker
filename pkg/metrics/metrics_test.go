package metrics

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paytm-hack/seatbooking/pkg/db/contract"
)

type availabilityReaderFunc func(context.Context) ([]contract.ShowSeatAvailability, error)

func (reader availabilityReaderFunc) ListSeatAvailability(ctx context.Context) ([]contract.ShowSeatAvailability, error) {
	return reader(ctx)
}

func TestMetricsExposeReservationCountersAndPerShowAvailability(t *testing.T) {
	metrics := New(availabilityReaderFunc(func(context.Context) ([]contract.ShowSeatAvailability, error) {
		return []contract.ShowSeatAvailability{
			{ShowID: "show-1", Available: 12},
			{ShowID: "show-2", Available: 0},
		}, nil
	}))
	metrics.ReservationConfirmed()
	metrics.ReservationDeclined(DeclineSeatTaken)
	metrics.ReservationDeclined(DeclinePerUserLimit)
	metrics.ReservationDeclined(DeclineIdempotentReplay)

	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	output := response.Body.String()

	for _, expected := range []string{
		"seatbooking_reservations_confirmed_total 1",
		`seatbooking_reservations_declined_total{reason="seat-taken"} 1`,
		`seatbooking_reservations_declined_total{reason="per-user-limit"} 1`,
		`seatbooking_reservations_declined_total{reason="idempotent-replay"} 1`,
		`seatbooking_seats_available{show_id="show-1"} 12`,
		`seatbooking_seats_available{show_id="show-2"} 0`,
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("metrics output does not contain %q:\n%s", expected, output)
		}
	}
}
