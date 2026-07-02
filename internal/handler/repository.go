package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/navikt/whodis/internal/github"
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

func (repo *Repository) SlackChannelsForRepo(w http.ResponseWriter, r *http.Request) {
	ctx, span := repo.Tracer.Start(r.Context(), "SlcakChannelsForRepo")
	defer span.End()
	repoName := r.PathValue("repoName")
	teams, err := repo.GitHubClient.TeamsFor(repoName, ctx)
	if err != nil {
		slog.Error("error getting teams for repo", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if len(teams) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
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
		if len(channels) == 0 {
			channelsFromTeamkatalogen, err := repo.allSlackChannelsForAllRepoAdminPeople(ctx, repoName)
			if err != nil {
				slog.Error("error getting channels for repo", slog.Any("error", err))
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			channels = append(channels, channelsFromTeamkatalogen...)
		}
	}
	if err := json.NewEncoder(w).Encode(channels); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (repo *Repository) allSlackChannelsForAllRepoAdminPeople(ctx context.Context, repoName string) ([]string, error) {
	repoAdmins, err := repo.GitHubClient.AdminsFor(repoName, ctx)
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
