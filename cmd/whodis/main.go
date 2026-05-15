package main

import (
	"context"
	"fmt"
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
		fmt.Println("failed to start app:", err)
	}
}
