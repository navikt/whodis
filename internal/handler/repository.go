package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/navikt/whodis/internal/github"
)

type Repository struct {
	GitHub github.Client
}

func (repo *Repository) Owners(w http.ResponseWriter, r *http.Request) {
	repoName := r.PathValue("repoName")
	owners, err := repo.GitHub.AdminsFor(repoName)
	if err != nil {
		slog.Error("error getting repo owners: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
	if err := json.NewEncoder(w).Encode(owners); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
	return
}
