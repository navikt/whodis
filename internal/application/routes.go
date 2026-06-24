package application

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httplog/v3"
	"github.com/go-chi/metrics"
	"github.com/navikt/whodis/internal/handler"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func (a *App) loadRoutes() {
	router := chi.NewRouter()

	instrumentedRouter := otelhttp.NewHandler(router, "whodis")
	router.Use(metrics.Collector(metrics.CollectorOpts{
		Host:  false,
		Proto: true,
		Skip: func(r *http.Request) bool {
			return r.Method != "OPTIONS"
		},
	}))

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
	router.Route("/", a.loadBusinessRoutes)

	a.router = instrumentedRouter
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
	router.Handle("/metrics", metrics.Handler())
}

func (a *App) loadBusinessRoutes(router chi.Router) {
	repoHandler := handler.Repository{
		GitHubClient: a.ghClient,
		Tracer:       a.tracer,
	}
	naisApiHandler := handler.NaisApi{
		NaisClient: a.nais,
		Tracer:     a.tracer,
	}

	router.Group(func(r chi.Router) {
		r.Get("/ghuser/{username}", repoHandler.EmailForGitHubUser)
		r.Get("/repository/{repoName}/deployments", repoHandler.Deployments)
		r.Get("/repository/{repoName}/admins", repoHandler.TeamsForAdmins)
		r.Get("/nais/{teamSlug}", naisApiHandler.DetailsForTeam)
	})
}
