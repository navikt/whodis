package main

import (
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/navikt/whodis/internal/auth"
	"github.com/navikt/whodis/internal/github"
	"github.com/navikt/whodis/internal/routes"
)

var port = ":8080"

func main() {
	wellKnownURI := envOrBust("WELL_KNOWN_URI")
	err := auth.Init(wellKnownURI)
	if err != nil {
		panic(err)
	}

	ghAppPrivateKey := envOrBust("GITHUB_APP_PRIVATE_KEY")
	ghAppClientId := envOrBust("GITHUB_APP_CLIENT_ID")
	ghAppInstallationId := envOrBust("GITHUB_APP_INSTALLATION_ID")
	err = github.Init(ghAppPrivateKey, ghAppClientId, ghAppInstallationId)
	if err != nil {
		panic(err)
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)

	r.Get("/", routes.GetRoot)
	r.Get("/internal/isalive", routes.GetLiveness)
	r.Get("/internal/isready", routes.GetReadyness)

	r.Group(func(r chi.Router) {
		r.Use(auth.JWTMiddleware)
		r.Get("/email/{githubUser}", routes.GetTest)
	})

	if err := http.ListenAndServe(port, r); err != nil {
		panic(err)
	}
}

func envOrBust(key string) string {
	value := os.Getenv(key)
	if value == "" {
		panic("unable not find environment variable " + key)
	}
	return value
}
