package application

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/navikt/whodis/internal/auth"
	"github.com/navikt/whodis/internal/github"
)

type App struct {
	router   http.Handler
	auth     *auth.Auth
	ghClient *github.Client
}

type config struct {
	WellKnownURI      string
	AppPrivateKeyPem  string
	AppClientID       string
	AppInstallationID string
}

func New() (*App, error) {
	config, err := configFromEnv()
	if err != nil {
		return nil, err
	}
	authn, err := auth.New(config.WellKnownURI)
	if err != nil {
		return nil, err
	}
	gh := github.New(config.AppPrivateKeyPem, config.AppClientID, config.AppInstallationID)
	app := &App{
		auth:     authn,
		ghClient: gh,
	}
	app.loadRoutes()
	return app, nil
}

func (a *App) Start(ctx context.Context) error {
	server := &http.Server{
		Addr:    ":8080",
		Handler: a.router,
	}

	fmt.Println("Starting server")

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
		"APP_PRIVATE_KEY_PEM",
		"APP_CLIENT_ID",
		"APP_INSTALLATION_ID",
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
	}, nil
}
