package github

import (
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/navikt/whodis/internal/cache"
	"github.com/navikt/whodis/internal/httpsupport"
)

const (
	repoTeamsTTL         = 15 * time.Minute
	repoTeamsNegativeTTL = 2 * time.Minute
	repoTeamsMaxEntries  = 10000

	// A team can have access to hundreds of repositories, so this is bounded tighter than repoTeamsMaxEntries.
	teamReposMaxEntries = 2000
)

// newRepoTeamsCache caches the unfiltered GitHub response, so the teamsToSkip filter and the
// admin/all split are applied to fresh configuration on every read. Negative entries (404 or no
// teams) are short-lived so a new repository or team assignment shows up quickly.
func newRepoTeamsCache() *cache.Cache[teamResponse] {
	return cache.New(cache.Options[teamResponse]{
		TTL:             repoTeamsTTL,
		NegativeTTL:     repoTeamsNegativeTTL,
		MaxEntries:      repoTeamsMaxEntries,
		Clone:           slices.Clone[teamResponse],
		IsNegativeError: isNotFound,
		IsNegativeValue: func(teams teamResponse) bool { return len(teams) == 0 },
	})
}

// newTeamReposCache holds the repository full names a GitHub team has access to. An unknown team
// (404) or a team without repositories is a short-lived negative entry.
func newTeamReposCache() *cache.Cache[[]string] {
	return cache.New(cache.Options[[]string]{
		TTL:             repoTeamsTTL,
		NegativeTTL:     repoTeamsNegativeTTL,
		MaxEntries:      teamReposMaxEntries,
		Clone:           slices.Clone[[]string],
		IsNegativeError: isNotFound,
		IsNegativeValue: func(repos []string) bool { return len(repos) == 0 },
	})
}

func isNotFound(err error) bool {
	var httpErr *httpsupport.HttpError
	return errors.As(err, &httpErr) && httpErr.Code == http.StatusNotFound
}
