package handler

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	"github.com/navikt/whodis/internal/nais"
	"go.opentelemetry.io/otel/trace"
)

type NaisApi struct {
	NaisClient   naisTeamClient
	GitHubClient naisGitHubClient
	Tracer       trace.Tracer
}

type naisTeamClient interface {
	DetailsFor(string, context.Context) (*nais.TeamDetails, error)
	RepositoriesFor(string, context.Context) ([]string, error)
}

type naisGitHubClient interface {
	ReposForTeam(string, context.Context) ([]string, error)
}

var re = regexp.MustCompile(`^[a-zA-Z0-9æøåÆØÅ\-_]{1,50}$`)

func (api *NaisApi) DetailsForTeam(w http.ResponseWriter, r *http.Request) {
	ctx, span := api.Tracer.Start(r.Context(), "DetailsForTeam")
	defer span.End()
	teamSlug := r.PathValue("teamSlug")
	if !re.MatchString(teamSlug) {
		invalidTeamSlug(w, r)
		return
	}
	teamDetails, err := api.NaisClient.DetailsFor(teamSlug, ctx)
	if errors.Is(err, nais.ErrTeamNotFound) {
		notFound(w, r, "team_not_found", "No NAIS team exists with this slug.")
		return
	}
	if err != nil {
		dependencyFailure(w, r, "error getting team details", err)
		return
	}
	writeJSON(w, r, teamDetails)
}

func (api *NaisApi) RepositoriesForTeam(w http.ResponseWriter, r *http.Request) {
	ctx, span := api.Tracer.Start(r.Context(), "RepositoriesForTeam")
	defer span.End()
	teamSlug := r.PathValue("teamSlug")
	if !re.MatchString(teamSlug) {
		invalidTeamSlug(w, r)
		return
	}
	naisRepos, err := api.NaisClient.RepositoriesFor(teamSlug, ctx)
	if errors.Is(err, nais.ErrTeamNotFound) {
		notFound(w, r, "team_not_found", "No NAIS team exists with this slug.")
		return
	}
	if err != nil {
		dependencyFailure(w, r, "error getting repositories for team from NAIS Console", err)
		return
	}
	// A NAIS team without a matching GitHub team simply has no GitHub-granted repositories.
	ghRepos, err := api.GitHubClient.ReposForTeam(teamSlug, ctx)
	if err != nil && !isHTTPNotFound(err) {
		dependencyFailure(w, r, "error getting repositories for team from GitHub", err)
		return
	}
	writeJSON(w, r, nonNil(mergeUnique(naisRepos, ghRepos)))
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
