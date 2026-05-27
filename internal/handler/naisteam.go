package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/navikt/whodis/internal/nais"
)

type NaisApi struct {
	NaisClient *nais.Api
}

func (api *NaisApi) DetailsForTeam(w http.ResponseWriter, r *http.Request) {
	teamSlug := r.PathValue("teamSlug")
	teamDetails, err := api.NaisClient.DetailsFor(teamSlug)
	if err != nil {
		slog.Error("error getting app details", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(teamDetails); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}
