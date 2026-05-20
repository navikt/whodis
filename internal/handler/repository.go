package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/navikt/whodis/internal/github"
	"github.com/navikt/whodis/internal/teamkatalogen"
)

type Repository struct {
	GitHubClient *github.Client
}

func (repo *Repository) Owners(w http.ResponseWriter, r *http.Request) {
	repoName := r.PathValue("repoName")
	owners, err := repo.GitHubClient.AdminsFor(repoName)
	if err != nil {
		slog.Error("error getting repo owners", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
	}
	var reply []ownerInfo
	for _, owner := range owners {
		ownerDetails, err := teamkatalogen.DetailsForUser(repo.GitHubClient.EmailFor(owner))
		if err != nil {
			slog.Error("error getting repo owner details", slog.Any("error", err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		reply = append(reply, ownerInfo{
			GitHubUsername: owner,
			WorkEmail:      repo.GitHubClient.EmailFor(owner),
			Teams:          ownerDetails.Teams,
		})
	}
	if err := json.NewEncoder(w).Encode(reply); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

type ownerInfo struct {
	GitHubUsername string               `json:"github_username"`
	WorkEmail      string               `json:"work_email"`
	Teams          []teamkatalogen.Team `json:"teams"`
}
