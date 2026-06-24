package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/navikt/whodis/internal/nais"
	"go.opentelemetry.io/otel/trace"
)

type NaisApi struct {
	NaisClient *nais.Api
	Tracer     trace.Tracer
}

var re = regexp.MustCompile(`^[a-zA-Z0-9æøåÆØÅ\-_]{1,50}$`)

func (api *NaisApi) DetailsForTeam(w http.ResponseWriter, r *http.Request) {
	ctx, span := api.Tracer.Start(r.Context(), "EmailForGitHubUser")
	defer span.End()
	teamSlug := r.PathValue("teamSlug")
	if !re.Match([]byte(teamSlug)) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	teamDetails, err := api.NaisClient.DetailsFor(teamSlug, ctx)
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
