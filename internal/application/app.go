package application

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/navikt/whodis/internal/github"
	"github.com/navikt/whodis/internal/nais"
)

type App struct {
	router   http.Handler
	ghClient *github.Client
	nais     *nais.Api
}

type config struct {
	WellKnownURI      string
	AppPrivateKeyPem  string
	AppClientID       string
	AppInstallationID string
	NaisApiKey        string
}

func New() (*App, error) {
	config, err := configFromEnv()
	if err != nil {
		return nil, err
	}
	gh := github.New(config.AppPrivateKeyPem, config.AppClientID, config.AppInstallationID)
	naisClient := nais.New(config.NaisApiKey)
	app := &App{
		ghClient: gh,
		nais:     naisClient,
	}
	app.loadRoutes()
	return app, nil
}

func (a *App) Start(ctx context.Context) error {
	server := &http.Server{
		Addr:    ":8080",
		Handler: a.router,
	}

	slog.Info("Starting server")

	ch := make(chan error, 1)

	err := a.ghClient.Ping()

	go func() {
		err := server.ListenAndServe()
		if err != nil {
			ch <- fmt.Errorf("failed to start server: %w", err)
		}
		close(ch)
	}()

	select {
	case err = <-ch:
		return err
	case <-ctx.Done():
		timeout, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()

		return server.Shutdown(timeout)
	}
}

func configFromEnv() (*config, error) {
	requiredVars := []string{
		"WELL_KNOWN_URI",
		"GITHUB_APP_PRIVATE_KEY",
		"GITHUB_APP_CLIENT_ID",
		"GITHUB_APP_INSTALLATION_ID",
		"NAIS_API_TOKEN",
	}
	m := map[string]string{}

	for _, v := range requiredVars {
		value := os.Getenv(v)
		if value == "" {
			return nil, fmt.Errorf("required env var %s is not set", v)
		}
		m[v] = value
	}
	return &config{
		WellKnownURI:      m["WELL_KNOWN_URI"],
		AppPrivateKeyPem:  m["GITHUB_APP_PRIVATE_KEY"],
		AppClientID:       m["GITHUB_APP_CLIENT_ID"],
		AppInstallationID: m["GITHUB_APP_INSTALLATION_ID"],
		NaisApiKey:        m["NAIS_API_TOKEN"],
	}, nil
}
