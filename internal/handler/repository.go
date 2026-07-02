package handler

import (
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

func (repo *Repository) AdminPeopleInfo(w http.ResponseWriter, r *http.Request) {
	ctx, span := repo.Tracer.Start(r.Context(), "TeamsForAdmins")
	defer span.End()
	repoName := r.PathValue("repoName")
	repoAdmins, err := repo.GitHubClient.AdminsFor(repoName, ctx)
	if err != nil {
		slog.Error("error getting repo admins", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	var allTeamkatalogenResponses []teamkatalogen.UserDetails
	var wg sync.WaitGroup
	wg.Add(len(repoAdmins))
	var mu sync.Mutex
	for _, admin := range repoAdmins {
		go func() {
			defer wg.Done()
			teamkatalogenReponse, err := teamkatalogen.DetailsForUser(repo.GitHubClient.EmailFor(admin), ctx)
			if err != nil {
				slog.Error("error getting repo admin details", slog.Any("error", err))
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			mu.Lock()
			allTeamkatalogenResponses = append(allTeamkatalogenResponses, *teamkatalogenReponse)
			mu.Unlock()
		}()
	}
	wg.Wait()
	unique := extractUnique(allTeamkatalogenResponses)
	w.Header().Set("Content-Type", "application/json")
	reply := TeamsForRepoAdminsReply{
		Users:         repo.enrichWithEmails(repoAdmins),
		Teams:         unique.Teams,
		SlackChannels: unique.SlackChannels,
	}
	if err := json.NewEncoder(w).Encode(reply); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (repo *Repository) SlackChannelsForRepo(w http.ResponseWriter, r *http.Request) {
	ctx, span := repo.Tracer.Start(r.Context(), "TeamsForRepo")
	defer span.End()
	repoName := r.PathValue("repoName")
	teams, err := repo.GitHubClient.TeamsFor(repoName, ctx)
	if err != nil {
		slog.Error("error getting teams for repo", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	var channels []string
	for _, team := range teams {
		naisTeamDetails, err := repo.NaisClient.DetailsFor(team, ctx)
		if err != nil {
			slog.Error("error getting channels repo", slog.Any("error", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if !slices.Contains(channels, naisTeamDetails.SlackChannel) {
			channels = append(channels, naisTeamDetails.SlackChannel)
		}
		if len(channels) == 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}
	if err := json.NewEncoder(w).Encode(channels); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (repo *Repository) Deployments(w http.ResponseWriter, r *http.Request) {
	ctx, span := repo.Tracer.Start(r.Context(), "Deployments")
	defer span.End()
	repoName := r.PathValue("repoName")
	w.Header().Set("Content-Type", "application/json")
	deployments, err := repo.GitHubClient.WhereIsItDeployed(repoName, ctx)
	if err != nil {
		slog.Error("error determining deployments", slog.Any("error", err))
		deployments = []github.NaisDeployment{}
	}
	if err := json.NewEncoder(w).Encode(deployments); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func extractUnique(fromTeamkatalogen []teamkatalogen.UserDetails) *uniqueThings {
	var uniqueTeams []string
	var uniqueSlackChannels []string

	for _, userDetails := range fromTeamkatalogen {
		for _, team := range userDetails.Teams {
			if !slices.Contains(uniqueTeams, team.Name) {
				uniqueTeams = append(uniqueTeams, team.Name)
			}
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
	return &uniqueThings{
		Teams:         uniqueTeams,
		SlackChannels: uniqueSlackChannels,
	}
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

func (repo *Repository) enrichWithEmails(usernames []string) []User {
	var users []User
	for _, username := range usernames {
		if username != "" {
			users = append(users, User{
				Username: username,
				Email:    repo.GitHubClient.EmailFor(username),
			})
		}
	}
	return users
}

type uniqueThings struct {
	Teams         []string
	SlackChannels []string
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
