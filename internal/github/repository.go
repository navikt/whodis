package github

import (
	"errors"
	"regexp"
	"strings"
)

type RepositoryID struct {
	owner string
	name  string
}

var repositoryNamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,100}$`)

func NewRepositoryID(owner, name string) (RepositoryID, error) {
	if !strings.EqualFold(owner, "navikt") {
		return RepositoryID{}, errors.New("only the navikt organization is supported")
	}
	if !repositoryNamePattern.MatchString(name) || name == "." || name == ".." {
		return RepositoryID{}, errors.New("repository name must contain 1 to 100 ASCII letters, digits, dots, underscores or hyphens, and cannot be \".\" or \"..\"")
	}
	return RepositoryID{owner: "navikt", name: name}, nil
}

func (r RepositoryID) String() string {
	return r.owner + "/" + r.name
}
