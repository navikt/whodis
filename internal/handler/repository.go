package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/navikt/whodis/internal/github"
	"github.com/navikt/whodis/internal/teamkatalogen"
)

type Repository struct {
	GitHubClient *github.Client
}

func (repo *Repository) Owners(w http.ResponseWriter, r *http.Request) {
	repoName := r.PathValue("repoName")
	repoAdmins, err := repo.GitHubClient.AdminsFor(repoName)
	if err != nil {
		slog.Error("error getting repo admins", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	var wg sync.WaitGroup
	wg.Add(len(repoAdmins))
	var reply []adminDetails
	for _, admin := range repoAdmins {
		go func() {
			teamkatalogenReponse, err := teamkatalogen.DetailsForUser(repo.GitHubClient.EmailFor(admin))
			defer wg.Done()
			if err != nil {
				slog.Error("error getting repo admin details", slog.Any("error", err))
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			reply = append(reply, adminDetails{
				GitHubUsername: admin,
				WorkEmail:      repo.GitHubClient.EmailFor(admin),
				Teams:          teamkatalogenReponse.Teams,
			})
		}()
	}
	wg.Wait()
	w.Header().Set("Content-Type", "application/json")
	_, err = repo.GitHubClient.SlackChannelFor(repoName)
	if err != nil {
		slog.Error("error determining slack channel", slog.Any("error", err))
	}
	if err := json.NewEncoder(w).Encode(reply); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

type adminDetails struct {
	GitHubUsername string               `json:"github_username"`
	WorkEmail      string               `json:"work_email"`
	Teams          []teamkatalogen.Team `json:"teams"`
}
