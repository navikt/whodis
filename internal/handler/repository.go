package handler

import (
	"encoding/json"
	"net/http"

	"github.com/navikt/whodis/internal/github"
)

type Repository struct {
	GitHub github.Client
}

func (repo *Repository) Owners(w http.ResponseWriter, r *http.Request) {
	repoName := r.PathValue("repoName")
	owners, err := repo.GitHub.OwnersFor(repoName)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
	if err := json.NewEncoder(w).Encode(owners); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
	return
}
