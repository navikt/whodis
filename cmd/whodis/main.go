package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	"github.com/navikt/whodis/internal/application"
)

func main() {
	app, err := application.New()
	if err != nil {
		panic(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := app.Start(ctx); err != nil {
		slog.Error("failed to start app:", err)
	}
}
