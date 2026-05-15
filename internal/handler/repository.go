package handler

import (
	"github.com/navikt/whodis/internal/github"
)

type Repository struct {
	gh github.Client
}

func (r *Repository) Owners(repoName string) error {
	return nil
}
