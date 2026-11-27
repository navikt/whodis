package handler

import (
	"context"
	"errors"
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
}

func (repo *Repository) EmailForGitHubUser(w http.ResponseWriter, r *http.Request) {
	_, span := repo.Tracer.Start(r.Context(), "EmailForGitHubUser")
	defer span.End()
	ghUser := r.PathValue("username")
	email := repo.GitHubClient.EmailFor(ghUser)
	if email == "" {
		notFound(w, r, "user_not_found", "No NAV email address is known for this GitHub user.")
		return
	}

	writeJSON(w, r, map[string]string{"email": email})
}

func (repo *Repository) OwnerTeamsForRepo(w http.ResponseWriter, r *http.Request) {
	repository, err := github.NewRepositoryID(r.PathValue("owner"), r.PathValue("repoName"))
	if err != nil {
		invalidRepository(w, r, err)
		return
	}
	ctx, span := repo.Tracer.Start(r.Context(), "OwnerTeamsForRepo")
	defer span.End()
	owners, err := repo.ownerTeamsForRepo(repository, ctx)
	if err != nil {
		handleRepositoryLookupError(err, w, r)
		return
	}
	if len(owners) != 0 {
		writeJSON(w, r, owners)
		return
	}

	slog.Info("no owners found on github, keep looking", slog.Any("owners", owners), slog.Any("repo", repository.String()))
	owners, err = repo.allTeamsForRepo(repository, ctx)
	if err != nil {
		handleRepositoryLookupError(err, w, r)
		return
	}

	writeJSON(w, r, nonNil(owners))
}

func handleRepositoryLookupError(err error, w http.ResponseWriter, r *http.Request) {
	if isHTTPNotFound(err) {
		notFound(w, r, "repository_not_found", "The repository does not exist or is not accessible to whodis.")
		return
	}
	dependencyFailure(w, r, "error looking up repository in GitHub", err)
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
	ctx, span := repo.Tracer.Start(r.Context(), "SlackChannelsForRepo")
	defer span.End()
	teams, err := repo.GitHubClient.AllTeamsFor(repository, ctx)
	if err != nil {
		handleRepositoryLookupError(err, w, r)
		return
	}
	if len(teams) == 0 {
		slog.Info("unable to find team in GitHub", slog.Any("repo", repository.String()))
	}
	channels := []string{}
	for _, team := range teams {
		naisTeamDetails, err := repo.NaisClient.DetailsFor(team, ctx)
		if errors.Is(err, nais.ErrTeamNotFound) {
			slog.Info("team was not found in nais", slog.Any("team", team))
			continue
		}
		if err != nil {
			dependencyFailure(w, r, "error getting channels for repo", err)
			return
		}
		if naisTeamDetails.SlackChannel != "" && !slices.Contains(channels, naisTeamDetails.SlackChannel) {
			channels = append(channels, naisTeamDetails.SlackChannel)
		}
	}
	if len(channels) == 0 {
		slog.Info("unable to find slack channels from GitHub team and nais, trying individuals and Teamkatalogen",
			slog.Any("repo", repository.String()))
		channelsFromTeamkatalogen, err := repo.allSlackChannelsForAllRepoAdminPeople(ctx, repository)
		if err != nil {
			dependencyFailure(w, r, "error getting channels for repo", err)
			return
		}
		channels = append(channels, channelsFromTeamkatalogen...)
	}
	if len(channels) == 0 {
		slog.Info("unable to find Slack channel(s)", slog.Any("repo", repository.String()))
	}
	writeJSON(w, r, channels)
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
