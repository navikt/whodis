package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/navikt/whodis/internal/nais"
)

type NaisApplication struct {
	NaisClient *nais.Api
}

func (app *NaisApplication) DetailsFor(w http.ResponseWriter, r *http.Request) {
	appName := r.PathValue("appName")
	appDetails, err := app.NaisClient.DetailsFor(appName)
	if err != nil {
		slog.Error("error getting app details", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(appDetails); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}
