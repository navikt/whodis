package application

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httplog/v3"
	"github.com/navikt/whodis/internal/handler"
)

func (a *App) loadRoutes() {
	router := chi.NewRouter()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}))
	slog.SetDefault(logger)

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
		if a.ghClient.SemiStaticDataIsLoaded() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusTeapot)
		}
	})
}

func (a *App) loadProtectedRoutes(router chi.Router) {
	repoHandler := handler.Repository{
		GitHubClient: a.ghClient,
	}
	naisApiHandler := handler.NaisApi{
		NaisClient: a.nais,
	}

	router.Group(func(r chi.Router) {
		r.Use(a.auth.JWTMiddleware)
		r.Get("/repository/{repoName}", repoHandler.Owners)
		r.Get("/nais/{teamSlug}", naisApiHandler.DetailsForTeam)
	})
}
