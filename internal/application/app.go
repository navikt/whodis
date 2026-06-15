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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type App struct {
	router   http.Handler
	ghClient *github.Client
	nais     *nais.Api
}

type config struct {
	AppPrivateKeyPem   string
	AppClientID        string
	AppInstallationID  string
	NaisApiKeyLocation string
}

func New() (*App, error) {
	config, err := configFromEnv()
	if err != nil {
		return nil, err
	}
	gh := github.New(config.AppPrivateKeyPem, config.AppClientID, config.AppInstallationID)
	naisClient := nais.New(config.NaisApiKeyLocation)

	exporter, err := otlptracegrpc.New(context.Background())
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second)),
	)
	otel.SetTracerProvider(provider)

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
	if err != nil {
		ch <- err
	}

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
		"GITHUB_APP_PRIVATE_KEY",
		"GITHUB_APP_CLIENT_ID",
		"GITHUB_APP_INSTALLATION_ID",
		"NAIS_SERVICE_ACCOUNT_TOKEN_PATH",
	}
	m := map[string]string{}

	for _, v := range requiredVars {
		value := os.Getenv(v)
		if value == "" {
			return nil, fmt.Errorf("required env var %s is not set", v)
		}
		m[v] = value
	}
	conf := &config{
		AppPrivateKeyPem:   m["GITHUB_APP_PRIVATE_KEY"],
		AppClientID:        m["GITHUB_APP_CLIENT_ID"],
		AppInstallationID:  m["GITHUB_APP_INSTALLATION_ID"],
		NaisApiKeyLocation: m["NAIS_SERVICE_ACCOUNT_TOKEN_PATH"],
	}
	return conf, nil
}
