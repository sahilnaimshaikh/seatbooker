package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/paytm-hack/seatbooking/pkg/httpjson"
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
				Msg("Details of " + r.URL.Path)
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

func RequireUser(secret string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			tokenString, ok := bearerToken(request)
			if !ok || secret == "" {
				httpjson.WriteError(writer, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
				return
			}

			claims := &jwt.RegisteredClaims{}
			_, err := jwt.ParseWithClaims(tokenString, claims, func(*jwt.Token) (any, error) {
				return []byte(secret), nil
			}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
			if err != nil || claims.Subject == "" {
				httpjson.WriteError(writer, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
				return
			}

			ctx := reqctx.WithUserID(request.Context(), claims.Subject)
			next.ServeHTTP(writer, request.WithContext(ctx))
		})
	}
}

func RequireAdmin(adminToken string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			token, ok := bearerToken(request)
			if !ok || adminToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(adminToken)) != 1 {
				httpjson.WriteError(writer, http.StatusUnauthorized, "unauthorized", "admin bearer token required")
				return
			}
			next.ServeHTTP(writer, request)
		})
	}
}

func bearerToken(request *http.Request) (string, bool) {
	value := request.Header.Get("Authorization")
	scheme, token, ok := strings.Cut(value, " ")
	return token, ok && strings.EqualFold(scheme, "Bearer") && token != "" && !strings.Contains(token, " ")
}
