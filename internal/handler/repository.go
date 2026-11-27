package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/go-chi/httplog/v3"
	"github.com/navikt/whodis/internal/github"
	"github.com/navikt/whodis/internal/httpsupport"
	"github.com/navikt/whodis/internal/nais"
	"github.com/navikt/whodis/internal/teamkatalogen"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

type Repository struct {
	GitHubClient repositoryGitHubClient
	NaisClient   repositoryNaisClient
	Tracer       trace.Tracer
}

type repositoryGitHubClient interface {
	EmailFor(string) string
	AdminTeamsFor(github.RepositoryID, context.Context) ([]string, error)
	AllTeamsFor(github.RepositoryID, context.Context) ([]string, error)
}

type repositoryNaisClient interface {
	DetailsFor(string, context.Context) (*nais.TeamDetails, error)
	TeamsForRepository(string, context.Context) ([]string, error)
}

func (repo *Repository) EmailForGitHubUser(w http.ResponseWriter, r *http.Request) {
	_, span := repo.Tracer.Start(r.Context(), "EmailForGitHubUser")
	defer span.End()
	ghUser := r.PathValue("username")
	email := repo.GitHubClient.EmailFor(ghUser)
	if email == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	reply := map[string]string{
		"email": email,
	}
	if err := json.NewEncoder(w).Encode(reply); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (repo *Repository) OwnerTeamsForRepo(w http.ResponseWriter, r *http.Request) {
	repository, err := github.NewRepositoryID(r.PathValue("owner"), r.PathValue("repoName"))
	if err != nil {
		invalidRepository(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	ctx, span := repo.Tracer.Start(r.Context(), "OwnerTeamsForRepo")
	defer span.End()
	owners, err := repo.ownerTeamsForRepo(repository, ctx)
	if err != nil {
		handlePossible404(err, w, r)
		return
	}
	if len(owners) != 0 {
		if err := json.NewEncoder(w).Encode(owners); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	slog.Info("no owners found on github, keep looking", slog.Any("owners", owners), slog.Any("repo", repository.String()))
	owners, err = repo.allTeamsForRepo(repository, ctx)
	if err != nil {
		handlePossible404(err, w, r)
		return
	}
	if len(owners) == 0 {
		owners, err = repo.NaisClient.TeamsForRepository(repository.String(), ctx)
		if err != nil {
			slog.Error("error getting owners from nais", slog.Any("error", err), slog.String("repo", repository.String()))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	if err := json.NewEncoder(w).Encode(owners); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func handlePossible404(err error, w http.ResponseWriter, r *http.Request) {
	var httpErr *httpsupport.HttpError
	if errors.As(err, &httpErr) && httpErr.Code == http.StatusNotFound {
		httplog.SetAttrs(r.Context(), slog.String("error.code", "resource_not_found"))
		w.WriteHeader(http.StatusNotFound)
		return
	}
	slog.Error("error getting owners for repo", slog.Any("error", err))
	w.WriteHeader(http.StatusInternalServerError)
}

func (repo *Repository) ownerTeamsForRepo(repository github.RepositoryID, ctx context.Context) ([]string, error) {
	owners, err := repo.GitHubClient.AdminTeamsFor(repository, ctx)
	if err != nil {
		return nil, err
	}

	return owners, nil
}

func (repo *Repository) allTeamsForRepo(repository github.RepositoryID, ctx context.Context) ([]string, error) {
	allTeams, err := repo.GitHubClient.AllTeamsFor(repository, ctx)
	if err != nil {
		return nil, err
	}

	return allTeams, nil
}

func (repo *Repository) SlackChannelsForRepo(w http.ResponseWriter, r *http.Request) {
	repository, err := github.NewRepositoryID(r.PathValue("owner"), r.PathValue("repoName"))
	if err != nil {
		invalidRepository(w, r, err)
		return
	}
	ctx, span := repo.Tracer.Start(r.Context(), "SlcakChannelsForRepo")
	defer span.End()
	teams, err := repo.GitHubClient.AllTeamsFor(repository, ctx)
	if err != nil {
		handlePossible404(err, w, r)
		return
	}
	if len(teams) == 0 {
		slog.Info("unable to find team in GitHub", slog.Any("repo", repository.String()))
	}
	channels, err := repo.slackChannelsForTeams(teams, ctx)
	if err != nil {
		slog.Error("error getting channels for repo", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if len(channels) == 0 {
		slog.Info("unable to find slack channels from GitHub teams, trying nais repository ownership",
			slog.String("repo", repository.String()))
		naisTeams, err := repo.NaisClient.TeamsForRepository(repository.String(), ctx)
		if err != nil {
			slog.Error("error getting repository teams from nais", slog.Any("error", err), slog.String("repo", repository.String()))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		channels, err = repo.slackChannelsForTeams(naisTeams, ctx)
		if err != nil {
			slog.Error("error getting channels for repo", slog.Any("error", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	if len(channels) == 0 {
		slog.Info("unable to find slack channels from GitHub team and nais, trying individuals and Teamkatalogen",
			slog.Any("repo", repository.String()))
		channelsFromTeamkatalogen, err := repo.allSlackChannelsForAllRepoAdminPeople(ctx, repository)
		if err != nil {
			slog.Error("error getting channels for repo", slog.Any("error", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		channels = append(channels, channelsFromTeamkatalogen...)
	}
	if len(channels) == 0 {
		slog.Info("unable to find Slack channel(s)", slog.Any("repo", repository.String()))
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err := json.NewEncoder(w).Encode(channels); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (repo *Repository) slackChannelsForTeams(teams []string, ctx context.Context) ([]string, error) {
	var channels []string
	for _, team := range teams {
		details, err := repo.NaisClient.DetailsFor(team, ctx)
		if err != nil {
			// Dodgy error handling, thanks GraphQL
			if strings.Contains(err.Error(), "specified team was not found") {
				slog.Info("team was not found in nais", slog.Any("team", team))
				continue
			}
			return nil, err
		}
		if strings.TrimSpace(details.SlackChannel) != "" && !slices.Contains(channels, details.SlackChannel) {
			channels = append(channels, details.SlackChannel)
		}
	}
	return channels, nil
}

func (repo *Repository) allSlackChannelsForAllRepoAdminPeople(ctx context.Context, repository github.RepositoryID) ([]string, error) {
	repoAdmins, err := repo.GitHubClient.AdminTeamsFor(repository, ctx)
	if err != nil {
		return nil, err
	}
	var allTeamkatalogenResponses []teamkatalogen.UserDetails
	var eg errgroup.Group
	var mu sync.Mutex
	for _, admin := range repoAdmins {
		eg.Go(func() error {
			teamkatalogenReponse, err := teamkatalogen.DetailsForUser(repo.GitHubClient.EmailFor(admin), ctx)
			if err != nil {
				return err
			}
			mu.Lock()
			allTeamkatalogenResponses = append(allTeamkatalogenResponses, *teamkatalogenReponse)
			mu.Unlock()
			return nil
		})
	}
	err = eg.Wait()
	if err != nil {
		return nil, err
	}
	uniqueSlackChannels := extractUniqueSlackChannels(allTeamkatalogenResponses)
	return uniqueSlackChannels, nil
}

func extractUniqueSlackChannels(fromTeamkatalogen []teamkatalogen.UserDetails) []string {
	var uniqueSlackChannels []string

	for _, userDetails := range fromTeamkatalogen {
		for _, team := range userDetails.Teams {
			// Some teams put several Slack channels separated by
			// space or comma i the field in Teamkatalogen
			splittedSlackChannels := splitSlackChannels(team.SlackChannel)
			for _, slackChannel := range splittedSlackChannels {
				if !slices.Contains(uniqueSlackChannels, slackChannel) {
					uniqueSlackChannels = append(uniqueSlackChannels, slackChannel)
				}
			}
		}
	}
	return uniqueSlackChannels
}

func splitSlackChannels(raw string) []string {
	if strings.Contains(raw, ",") {
		comma := regexp.MustCompile(",")
		splitted := comma.Split(raw, -1)
		var trimmed []string
		for _, s := range splitted {
			trimmed = append(trimmed, strings.TrimSpace(s))
		}
		return trimmed
	}
	if strings.Contains(raw, " ") {
		spaces := regexp.MustCompile(`\s+`)
		return spaces.Split(raw, -1)
	}

	return []string{raw}
}

type TeamsForRepoAdminsReply struct {
	Users         []User   `json:"users"`
	Teams         []string `json:"members_of"`
	SlackChannels []string `json:"slack_channels"`
}

type User struct {
	Username string `json:"username"`
	Email    string `json:"email"`
}
