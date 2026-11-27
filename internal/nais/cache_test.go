package nais

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	teamDetailsResponse = `{"data":{"team":{"slug":"appsec","slackChannel":"#appsec","purpose":"security","members":{"nodes":[
		{"user":{"email":"a@nav.no","name":"A"},"role":"OWNER"},
		{"user":{"email":"b@nav.no","name":"B"},"role":"MEMBER"}]}}}}`
	teamNotFoundResponse = `{"errors":[{"message":"The specified team was not found."}]}`
	teamsForRepoResponse = `{"data":{"teams":{"nodes":[
		{"slug":"one","repositories":{"nodes":[{"name":"navikt/dp-meldekort"}]}}],"pageInfo":{"hasNextPage":false}}}}`
	noTeamsResponse     = `{"data":{"teams":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}`
	accessDeniedGqlBody = `{"errors":[{"message":"access denied"}]}`
)

// cachedApi serves the given bodies in order (repeating the last) and returns the upstream request count.
func cachedApi(t *testing.T, bodies ...string) (*Api, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	mockNaisHTTP(t, func(w http.ResponseWriter, _ *http.Request) {
		i := int(requests.Add(1)) - 1
		if i >= len(bodies) {
			i = len(bodies) - 1
		}
		_, _ = w.Write([]byte(bodies[i]))
	})
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}
	return New(tokenPath), &requests
}

func TestTeamsForRepositoryIsCached(t *testing.T) {
	api, requests := cachedApi(t, teamsForRepoResponse)

	first, err := api.TeamsForRepository("navikt/dp-meldekort", context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first[0] = "mutated"
	second, err := api.TeamsForRepository("navikt/dp-meldekort", context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(second, []string{"one"}) {
		t.Errorf("cached value was corrupted by a caller: %v", second)
	}
	if requests.Load() != 1 {
		t.Errorf("expected 1 upstream request, got %d", requests.Load())
	}
}

func TestTeamsForRepositoryEmptyResultIsCached(t *testing.T) {
	api, requests := cachedApi(t, noTeamsResponse)

	for range 2 {
		teams, err := api.TeamsForRepository("navikt/dp-meldekort", context.Background())
		if err != nil || len(teams) != 0 {
			t.Fatalf("expected no teams, got %v, %v", teams, err)
		}
	}
	if requests.Load() != 1 {
		t.Errorf("expected 1 upstream request, got %d", requests.Load())
	}
}

func TestTeamsForRepositoryFailureIsNotCached(t *testing.T) {
	api, requests := cachedApi(t, accessDeniedGqlBody, teamsForRepoResponse)

	if _, err := api.TeamsForRepository("navikt/dp-meldekort", context.Background()); err == nil {
		t.Fatal("expected the first call to fail")
	}
	teams, err := api.TeamsForRepository("navikt/dp-meldekort", context.Background())
	if err != nil || !reflect.DeepEqual(teams, []string{"one"}) {
		t.Fatalf("expected recovery after a failure, got %v, %v", teams, err)
	}
	if requests.Load() != 2 {
		t.Errorf("expected 2 upstream requests, got %d", requests.Load())
	}
}

func TestDetailsForIsCachedPerTeam(t *testing.T) {
	api, requests := cachedApi(t, teamDetailsResponse)

	first, err := api.DetailsFor("appsec", context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first.Members[0].Name = "mutated"
	first.Slug = "mutated"
	second, err := api.DetailsFor("appsec", context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if second.Slug != "appsec" || second.Members[0].Name != "A" || len(second.Members) != 2 {
		t.Errorf("cached details were corrupted by a caller: %+v", second)
	}
	if requests.Load() != 1 {
		t.Errorf("expected 1 upstream request, got %d", requests.Load())
	}

	if _, err := api.DetailsFor("other-team", context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Errorf("a different team must not share the entry, got %d requests", requests.Load())
	}
}

func TestDetailsForTeamNotFoundIsCachedAndKeepsErrorText(t *testing.T) {
	api, requests := cachedApi(t, teamNotFoundResponse)

	for range 2 {
		_, err := api.DetailsFor("ghost", context.Background())
		// The handler matches on this text to skip unknown teams.
		if err == nil || !strings.Contains(err.Error(), "specified team was not found") {
			t.Fatalf("expected a team-not-found error, got %v", err)
		}
	}
	if requests.Load() != 1 {
		t.Errorf("expected the not-found result to be cached, got %d requests", requests.Load())
	}
}

func TestDetailsForOtherGraphQLErrorsAreNotCached(t *testing.T) {
	api, requests := cachedApi(t, accessDeniedGqlBody)

	for range 2 {
		if _, err := api.DetailsFor("appsec", context.Background()); err == nil {
			t.Fatal("expected an error")
		}
	}
	if requests.Load() != 2 {
		t.Errorf("expected 2 upstream requests, got %d", requests.Load())
	}
}

const (
	teamRepositoriesResponse = `{"data":{"team":{"slug":"appsec","repositories":{"nodes":[{"name":"navikt/a"},{"name":"navikt/b"}],"pageInfo":{"hasNextPage":false}}}}}`
	teamWithoutSlugResponse  = `{"data":{"team":{"slug":"","repositories":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`
)

func TestRepositoriesForIsCachedAndCopySafe(t *testing.T) {
	api, requests := cachedApi(t, teamRepositoriesResponse)

	first, err := api.RepositoriesFor("appsec", context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first[0] = "mutated"
	second, err := api.RepositoriesFor("appsec", context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(second, []string{"navikt/a", "navikt/b"}) {
		t.Errorf("cached value was corrupted by a caller: %v", second)
	}
	if requests.Load() != 1 {
		t.Errorf("expected 1 upstream request, got %d", requests.Load())
	}
}

func TestRepositoriesForUnknownTeamIsCachedAsErrTeamNotFound(t *testing.T) {
	api, requests := cachedApi(t, teamNotFoundResponse)

	for range 2 {
		if _, err := api.RepositoriesFor("ghost", context.Background()); !errors.Is(err, ErrTeamNotFound) {
			t.Fatalf("expected ErrTeamNotFound, got %v", err)
		}
	}
	if requests.Load() != 1 {
		t.Errorf("expected the not-found result to be cached, got %d requests", requests.Load())
	}
}

func TestRepositoriesForTeamWithoutSlugIsCached(t *testing.T) {
	api, requests := cachedApi(t, teamWithoutSlugResponse)

	for range 2 {
		repos, err := api.RepositoriesFor("ghost", context.Background())
		if err != nil || len(repos) != 0 {
			t.Fatalf("expected no repositories, got %v, %v", repos, err)
		}
	}
	if requests.Load() != 1 {
		t.Errorf("expected 1 upstream request, got %d", requests.Load())
	}
}

func TestRepositoriesForFailureIsNotCached(t *testing.T) {
	api, requests := cachedApi(t, accessDeniedGqlBody, teamRepositoriesResponse)

	if _, err := api.RepositoriesFor("appsec", context.Background()); err == nil {
		t.Fatal("expected the first call to fail")
	}
	repos, err := api.RepositoriesFor("appsec", context.Background())
	if err != nil || len(repos) != 2 {
		t.Fatalf("expected recovery after a failure, got %v, %v", repos, err)
	}
	if requests.Load() != 2 {
		t.Errorf("expected 2 upstream requests, got %d", requests.Load())
	}
}

func TestTeamsForRepositoryReadsFreshRepositoriesWhenPaginated(t *testing.T) {
	const (
		staleRepositories = `{"data":{"team":{"slug":"late","repositories":{"nodes":[{"name":"navikt/old"}],"pageInfo":{"hasNextPage":false}}}}}`
		paginatedTeams    = `{"data":{"teams":{"nodes":[{"slug":"late","repositories":{"nodes":[{"name":"navikt/old"}],"pageInfo":{"hasNextPage":true}}}],"pageInfo":{"hasNextPage":false}}}}`
		freshRepositories = `{"data":{"team":{"slug":"late","repositories":{"nodes":[{"name":"navikt/old"},{"name":"navikt/dp-meldekort"}],"pageInfo":{"hasNextPage":false}}}}}`
	)
	api, _ := cachedApi(t, staleRepositories, paginatedTeams, freshRepositories)

	if _, err := api.RepositoriesFor("late", context.Background()); err != nil {
		t.Fatal(err)
	}
	teams, err := api.TeamsForRepository("navikt/dp-meldekort", context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(teams, []string{"late"}) {
		t.Errorf("expected the reverse lookup to use fresh repositories, got %v", teams)
	}
}
