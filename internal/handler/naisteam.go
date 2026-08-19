package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/navikt/whodis/internal/github"
	"github.com/navikt/whodis/internal/nais"
	"go.opentelemetry.io/otel/trace"
)

type NaisApi struct {
	NaisClient   *nais.Api
	GitHubClient *github.Client
	Tracer       trace.Tracer
}

var re = regexp.MustCompile(`^[a-zA-Z0-9æøåÆØÅ\-_]{1,50}$`)

func (api *NaisApi) DetailsForTeam(w http.ResponseWriter, r *http.Request) {
	ctx, span := api.Tracer.Start(r.Context(), "DetailsForTeam")
	defer span.End()
	teamSlug := r.PathValue("teamSlug")
	if !re.Match([]byte(teamSlug)) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	teamDetails, err := api.NaisClient.DetailsFor(teamSlug, ctx)
	if err != nil {
		slog.Error("error getting team details", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(teamDetails); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (api *NaisApi) RepositoriesForTeam(w http.ResponseWriter, r *http.Request) {
	ctx, span := api.Tracer.Start(r.Context(), "RepositoriesForTeam")
	defer span.End()
	teamSlug := r.PathValue("teamSlug")
	if !re.Match([]byte(teamSlug)) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	naisRepos, err := api.NaisClient.RepositoriesFor(teamSlug, ctx)
	if err != nil {
		if errors.Is(err, nais.ErrTeamNotFound) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		slog.Error("error getting repositories for team from NAIS Console", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	ghRepos, err := api.GitHubClient.ReposForTeam(teamSlug, ctx)
	if err != nil {
		slog.Error("error getting repositories for team from GitHub", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	repos := mergeUnique(naisRepos, ghRepos)
	if len(repos) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(repos); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func mergeUnique(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	var result []string
	for _, s := range append(a, b...) {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	return result
}
