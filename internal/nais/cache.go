package nais

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/navikt/whodis/internal/cache"
	"github.com/navikt/whodis/internal/httpsupport"
)

const (
	cacheTTL         = 15 * time.Minute
	cacheNegativeTTL = 2 * time.Minute

	// TeamsForRepository pages through every Nais team, so its results are worth keeping longer.
	repositoryTeamsTTL = 30 * time.Minute
	// Entries are small slug lists, so the bound can be generous.
	repositoryTeamsMaxEntries = 10000
	// Team details include up to 50 members each, so the bound is tighter to protect memory.
	teamDetailsMaxEntries = 2000
	// A team can own hundreds of repositories, so this is bounded like team details.
	teamRepositoriesMaxEntries = 2000

	teamNotFoundMessage = "specified team was not found"
)

func newTeamDetailsCache() *cache.Cache[*TeamDetails] {
	return cache.New(cache.Options[*TeamDetails]{
		TTL:         cacheTTL,
		NegativeTTL: cacheNegativeTTL,
		MaxEntries:  teamDetailsMaxEntries,
		Clone: func(d *TeamDetails) *TeamDetails {
			if d == nil {
				return nil
			}
			clone := *d
			clone.Members = slices.Clone(d.Members)
			return &clone
		},
		IsNegativeError: func(err error) bool {
			var gqlErr *httpsupport.GqlError
			return errors.As(err, &gqlErr) && strings.Contains(gqlErr.Message, teamNotFoundMessage)
		},
		IsNegativeValue: func(d *TeamDetails) bool { return d == nil || d.Slug == "" },
	})
}

func newRepositoryTeamsCache() *cache.Cache[[]string] {
	return cache.New(cache.Options[[]string]{
		TTL:             repositoryTeamsTTL,
		NegativeTTL:     cacheNegativeTTL,
		MaxEntries:      repositoryTeamsMaxEntries,
		Clone:           slices.Clone[[]string],
		IsNegativeValue: func(teams []string) bool { return len(teams) == 0 },
	})
}

// newRepositoriesCache holds a team's repository names. Unknown teams and teams without
// repositories are both negative entries, matching how the handler reports them as 404.
func newRepositoriesCache() *cache.Cache[[]string] {
	return cache.New(cache.Options[[]string]{
		TTL:             cacheTTL,
		NegativeTTL:     cacheNegativeTTL,
		MaxEntries:      teamRepositoriesMaxEntries,
		Clone:           slices.Clone[[]string],
		IsNegativeError: func(err error) bool { return errors.Is(err, ErrTeamNotFound) },
		IsNegativeValue: func(repos []string) bool { return len(repos) == 0 },
	})
}

func (api *Api) CacheStats() map[string]cache.Stats {
	stats := make(map[string]cache.Stats)
	if api.detailsCache != nil {
		stats["nais_team_details"] = api.detailsCache.Stats()
	}
	if api.repositoryTeamsCache != nil {
		stats["nais_repository_teams"] = api.repositoryTeamsCache.Stats()
	}
	if api.repositoriesCache != nil {
		stats["nais_team_repositories"] = api.repositoriesCache.Stats()
	}
	return stats
}
