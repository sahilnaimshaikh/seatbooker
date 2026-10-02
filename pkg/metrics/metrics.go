package metrics

import (
	"context"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/paytm-hack/seatbooking/pkg/db/contract"
)

const (
	DeclineSeatTaken        = "seat-taken"
	DeclinePerUserLimit     = "per-user-limit"
	DeclineIdempotentReplay = "idempotent-replay"
	DeclineLockTimeout      = "lock-timeout"
)

type AvailabilityReader interface {
	ListSeatAvailability(ctx context.Context) ([]contract.ShowSeatAvailability, error)
}

type Metrics struct {
	registry  *prometheus.Registry
	confirmed prometheus.Counter
	declined  *prometheus.CounterVec
}

func New(availabilityReader AvailabilityReader) *Metrics {
	registry := prometheus.NewRegistry()
	confirmed := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "seatbooking",
		Name:      "reservations_confirmed_total",
		Help:      "Total number of newly confirmed reservations.",
	})
	declined := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "seatbooking",
		Name:      "reservations_declined_total",
		Help:      "Total reservation attempts not counted as new confirmations, by reason.",
	}, []string{"reason"})
	registry.MustRegister(confirmed, declined, &seatAvailabilityCollector{reader: availabilityReader})

	return &Metrics{
		registry:  registry,
		confirmed: confirmed,
		declined:  declined,
	}
}

func (m *Metrics) ReservationConfirmed() {
	m.confirmed.Inc()
}

func (m *Metrics) ReservationDeclined(reason string) {
	m.declined.WithLabelValues(reason).Inc()
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

type seatAvailabilityCollector struct {
	reader AvailabilityReader
}

func (c *seatAvailabilityCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- prometheus.NewDesc(
		"seatbooking_seats_available",
		"Number of available seats, grouped by show.",
		[]string{"show_id"},
		nil,
	)
}

func (c *seatAvailabilityCollector) Collect(ch chan<- prometheus.Metric) {
	if c.reader == nil {
		return
	}

	desc := prometheus.NewDesc(
		"seatbooking_seats_available",
		"Number of available seats, grouped by show.",
		[]string{"show_id"},
		nil,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	availability, err := c.reader.ListSeatAvailability(ctx)
	if err != nil {
		ch <- prometheus.NewInvalidMetric(desc, err)
		return
	}
	for _, show := range availability {
		metric, err := prometheus.NewConstMetric(desc, prometheus.GaugeValue, float64(show.Available), show.ShowID)
		if err != nil {
			ch <- prometheus.NewInvalidMetric(desc, err)
			return
		}
		ch <- metric
	}
}
