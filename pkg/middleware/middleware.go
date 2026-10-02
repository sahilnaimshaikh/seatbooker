package middleware

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/paytm-hack/seatbooking/pkg/logctx"
	"github.com/paytm-hack/seatbooking/pkg/reqctx"
)

type Middleware func(http.Handler) http.Handler

func Wrap(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		ctx := reqctx.WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func Logging(base zerolog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			requestID := reqctx.RequestID(r.Context())

			logger := base.With().Str("request_id", requestID).Logger()
			ctx := logctx.WithLogger(r.Context(), logger)

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r.WithContext(ctx))

			logger.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", rec.status).
				Str("user_id", reqctx.UserID(ctx)).
				Dur("latency_ms", time.Since(start)).
				Msg("request_handled")
		})
	}
}

func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				logger := logctx.FromContext(r.Context())
				logger.Error().Interface("panic", err).Msg("panic_recovered")
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
