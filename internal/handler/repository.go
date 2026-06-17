package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"sync"

	"github.com/navikt/whodis/internal/github"
	"github.com/navikt/whodis/internal/teamkatalogen"
)

type Repository struct {
	GitHubClient *github.Client
}

func (repo *Repository) EmailForGitHubUser(w http.ResponseWriter, r *http.Request) {
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

func (repo *Repository) TeamsForAdmins(w http.ResponseWriter, r *http.Request) {
	repoName := r.PathValue("repoName")
	repoAdmins, err := repo.GitHubClient.AdminsFor(repoName)
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
			teamkatalogenReponse, err := teamkatalogen.DetailsForUser(repo.GitHubClient.EmailFor(admin))
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
		Usernames:     repoAdmins,
		Teams:         unique.Teams,
		SlackChannels: unique.SlackChannels,
	}
	if err := json.NewEncoder(w).Encode(reply); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (repo *Repository) Deployments(w http.ResponseWriter, r *http.Request) {
	repoName := r.PathValue("repoName")
	w.Header().Set("Content-Type", "application/json")
	deployments, err := repo.GitHubClient.WhereIsItDeployed(repoName)
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
			if !slices.Contains(uniqueSlackChannels, team.SlackChannel) {
				uniqueSlackChannels = append(uniqueSlackChannels, team.SlackChannel)
			}
		}
	}
	return &uniqueThings{
		Teams:         uniqueTeams,
		SlackChannels: uniqueSlackChannels,
	}
}

type uniqueThings struct {
	Teams         []string
	SlackChannels []string
}

type TeamsForRepoAdminsReply struct {
	Usernames     []string `json:"usernames"`
	Teams         []string `json:"members_of"`
	SlackChannels []string `json:"slack_channels"`
}
