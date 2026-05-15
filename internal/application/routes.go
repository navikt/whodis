package application

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httplog/v3"
)

func (a *App) loadRoutes() {
	router := chi.NewRouter()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}))

	router.Use(httplog.RequestLogger(logger, &httplog.Options{
		Level:         slog.LevelInfo,
		Schema:        httplog.SchemaOTEL,
		RecoverPanics: true,

		Skip: func(req *http.Request, respStatus int) bool {
			return req.RequestURI == "/internal/isready" || req.RequestURI == "/internal/isalive"
		},
	}))

	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	router.Route("/internal", a.loadNaisRoutes)
	router.Route("/", a.loadProtectedRoutes)

	a.router = router
}

func (a *App) loadNaisRoutes(router chi.Router) {
	router.Get("/isalive", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	router.Get("/isready", func(w http.ResponseWriter, r *http.Request) {
		if a.ghClient.UsersAreLoaded() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusTeapot)
		}
	})
}

func (a *App) loadProtectedRoutes(router chi.Router) {
	router.Group(func(r chi.Router) {
		r.Use(a.auth.JWTMiddleware)
		r.Get("/yolo", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})
}
