package router

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/paytm-hack/seatbooking/pkg/config"
	"github.com/paytm-hack/seatbooking/pkg/handler"
	"github.com/paytm-hack/seatbooking/pkg/logctx"
	"github.com/paytm-hack/seatbooking/pkg/middleware"
)

const (
	LoginPath             = "/auth/login"
	CreateShowPath        = "/shows"
	GetShowPath           = "/shows/{id}"
	ReserveSeatPath       = "/shows/{id}/reserve"
	CancelReservationPath = "/reservations/{id}/cancel"
	LivenessPath          = "/health"
	ReadinessPath         = "/ready"
)

type Router struct {
	Shows        handler.ShowService
	Reservations handler.ReservationService
	Database     handler.ReadinessChecker
	AppConfig    *config.Config
	Context      context.Context
}

func New(r *Router) http.Handler {
	router := mux.NewRouter()
	logger := logctx.FromContext(r.Context)

	middlewares := []middleware.Middleware{
		middleware.WithRequestID,
		middleware.Logging(logger),
		middleware.Recover,
	}
	middlewaresWithUserAuth := append(middlewares, middleware.RequireUser(r.AppConfig.GetJWTSecret()))
	middlewaresWithAdminAuth := append(middlewares, middleware.RequireAdmin(r.AppConfig.GetAdminToken()))

	router.Handle(LoginPath, middleware.Wrap(handler.Login(handler.AuthConfig{
		JWTSecret: r.AppConfig.GetJWTSecret(),
		JWTExpiry: r.AppConfig.GetJWTExpiry(),
	}), middlewares...)).Methods(http.MethodPost)
	router.Handle(CreateShowPath, middleware.Wrap(handler.CreateShow(r.Shows), middlewaresWithAdminAuth...)).Methods(http.MethodPost)
	router.Handle(GetShowPath, middleware.Wrap(handler.GetShow(r.Shows), middlewares...)).Methods(http.MethodGet)
	router.Handle(ReserveSeatPath, middleware.Wrap(handler.ReserveSeat(r.Reservations), middlewaresWithUserAuth...)).Methods(http.MethodPost)
	router.Handle(CancelReservationPath, middleware.Wrap(handler.CancelReservation(r.Reservations), middlewaresWithUserAuth...)).Methods(http.MethodPost)
	router.Handle(LivenessPath, middleware.Wrap(handler.Liveness(), middlewares...)).Methods(http.MethodGet)
	router.Handle(ReadinessPath, middleware.Wrap(handler.Readiness(r.Database), middlewares...)).Methods(http.MethodGet)

	return router
}
