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

	"github.com/navikt/whodis/internal/github"
	"github.com/navikt/whodis/internal/httpsupport"
	"github.com/navikt/whodis/internal/nais"
	"github.com/navikt/whodis/internal/teamkatalogen"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

type Repository struct {
	GitHubClient *github.Client
	NaisClient   *nais.Api
	Tracer       trace.Tracer
}

var notFoundError = &httpsupport.HttpError{Code: 404}

var repoNameRegex = regexp.MustCompile(`^[a-zA-Z0-9æøåÆØÅ\-_]{1,50}$`)

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
	repoName := r.PathValue("repoName")
	if !repoNameRegex.Match([]byte(repoName)) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	ctx, span := repo.Tracer.Start(r.Context(), "OwnerTeamsForRepo")
	defer span.End()
	owners, err := repo.ownerTeamsForRepo(repoName, ctx)
	if err != nil {
		handlePossible404(err, w)
		return
	}
	if len(owners) != 0 {
		if err := json.NewEncoder(w).Encode(owners); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}

	slog.Info("no owners found on github, keep looking", slog.Any("owners", owners), slog.Any("repo", repoName))
	owners, err = repo.allTeamsForRepo(repoName, ctx)
	if err != nil {
		handlePossible404(err, w)
		return
	}

	if err := json.NewEncoder(w).Encode(owners); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func handlePossible404(err error, w http.ResponseWriter) {
	slog.Error("error getting owners for repo", slog.Any("error", err))
	status := http.StatusInternalServerError
	if errors.Is(err, notFoundError) {
		status = http.StatusNotFound
	}
	w.WriteHeader(status)
}

func (repo *Repository) ownerTeamsForRepo(repoName string, ctx context.Context) ([]string, error) {
	owners, err := repo.GitHubClient.AdminTeamsFor(repoName, ctx)
	if err != nil {
		return nil, err
	}

	return owners, nil
}

func (repo *Repository) allTeamsForRepo(repoName string, ctx context.Context) ([]string, error) {
	allTeams, err := repo.GitHubClient.AllTeamsFor(repoName, ctx)
	if err != nil {
		return nil, err
	}

	return allTeams, nil
}

func (repo *Repository) SlackChannelsForRepo(w http.ResponseWriter, r *http.Request) {
	ctx, span := repo.Tracer.Start(r.Context(), "SlcakChannelsForRepo")
	defer span.End()
	repoName := r.PathValue("repoName")
	teams, err := repo.GitHubClient.AllTeamsFor(repoName, ctx)
	if err != nil {
		slog.Error("error getting teams for repo", slog.Any("error", err))
		status := http.StatusInternalServerError
		if errors.Is(err, notFoundError) {
			status = http.StatusNotFound
		}
		w.WriteHeader(status)
		return
	}
	if len(teams) == 0 {
		slog.Info("unable to find team in GitHub", slog.Any("repo", repoName))
	}
	var channels []string
	for _, team := range teams {
		naisTeamDetails, err := repo.NaisClient.DetailsFor(team, ctx)
		if err != nil {
			// Dodgy error handling, thanks GraphQL
			if strings.Contains(err.Error(), "specified team was not found") {
				slog.Info("team was not found in nais", slog.Any("team", team))
				continue
			}
			slog.Error("error getting channels for repo", slog.Any("error", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if !slices.Contains(channels, naisTeamDetails.SlackChannel) {
			channels = append(channels, naisTeamDetails.SlackChannel)
		}
	}
	if len(channels) == 0 {
		slog.Info("unable to find slack channels from GitHub team and nais, trying individuals and Teamkatalogen",
			slog.Any("repo", repoName))
		channelsFromTeamkatalogen, err := repo.allSlackChannelsForAllRepoAdminPeople(ctx, repoName)
		if err != nil {
			slog.Error("error getting channels for repo", slog.Any("error", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		channels = append(channels, channelsFromTeamkatalogen...)
	}
	if len(channels) == 0 {
		slog.Info("unable to find Slack channel(s)", slog.Any("repo", repoName))
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err := json.NewEncoder(w).Encode(channels); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (repo *Repository) allSlackChannelsForAllRepoAdminPeople(ctx context.Context, repoName string) ([]string, error) {
	repoAdmins, err := repo.GitHubClient.AdminTeamsFor(repoName, ctx)
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
