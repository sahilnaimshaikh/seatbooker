package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/paytm-hack/seatbooking/pkg/httpjson"
)

const readinessTimeout = 3 * time.Second

type ReadinessChecker interface {
	PingContext(ctx context.Context) error
}

func Liveness() http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		httpjson.Write(writer, http.StatusOK, HealthResponse{Status: "ok"})
	}
}

func Readiness(database ReadinessChecker) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if database == nil {
			httpjson.WriteError(writer, http.StatusServiceUnavailable, "not_ready", "database unavailable")
			return
		}

		ctx, cancel := context.WithTimeout(request.Context(), readinessTimeout)
		defer cancel()
		if err := database.PingContext(ctx); err != nil {
			httpjson.WriteError(writer, http.StatusServiceUnavailable, "not_ready", "database unavailable")
			return
		}
		httpjson.Write(writer, http.StatusOK, HealthResponse{Status: "ready"})
	}
}
