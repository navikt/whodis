package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/navikt/whodis/internal/httpsupport"
	"github.com/navikt/whodis/internal/nais"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestRepositoriesForTeam_InvalidSlug(t *testing.T) {
	tests := []struct {
		name     string
		teamSlug string
	}{
		{"empty slug", ""},
		{"slug too long", "this-slug-is-way-too-long-to-be-valid-because-it-exceeds-fifty-chars"},
		{"slug with special chars", "team/slug"},
	}

	handler := &NaisApi{
		NaisClient:   nil,
		GitHubClient: nil,
		Tracer:       noop.NewTracerProvider().Tracer("test"),
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/nais/"+tt.teamSlug+"/repositories", nil)
			req.SetPathValue("teamSlug", tt.teamSlug)
			w := httptest.NewRecorder()

			handler.RepositoriesForTeam(w, req)

			assertProblem(t, w, http.StatusBadRequest, "invalid_team_slug")
		})
	}
}

type naisTeamStub struct {
	details *nais.TeamDetails
	repos   []string
	err     error
}

func (s naisTeamStub) DetailsFor(string, context.Context) (*nais.TeamDetails, error) {
	return s.details, s.err
}

func (s naisTeamStub) RepositoriesFor(string, context.Context) ([]string, error) {
	return s.repos, s.err
}

type naisGitHubStub struct {
	repos []string
	err   error
}

func (s naisGitHubStub) ReposForTeam(string, context.Context) ([]string, error) {
	return s.repos, s.err
}

func serveNais(api *NaisApi, handle func(*NaisApi, http.ResponseWriter, *http.Request), slug string) *httptest.ResponseRecorder {
	api.Tracer = noop.NewTracerProvider().Tracer("test")
	req := httptest.NewRequest(http.MethodGet, "/nais/"+slug, nil)
	req.SetPathValue("teamSlug", slug)
	w := httptest.NewRecorder()
	handle(api, w, req)
	return w
}

func TestDetailsForTeam(t *testing.T) {
	t.Run("team not found", func(t *testing.T) {
		w := serveNais(&NaisApi{NaisClient: naisTeamStub{err: nais.ErrTeamNotFound}}, (*NaisApi).DetailsForTeam, "missing")
		assertProblem(t, w, http.StatusNotFound, "team_not_found")
	})
	t.Run("NAIS Console error", func(t *testing.T) {
		w := serveNais(&NaisApi{NaisClient: naisTeamStub{err: &httpsupport.GqlError{Message: "boom"}}}, (*NaisApi).DetailsForTeam, "team")
		assertProblem(t, w, http.StatusBadGateway, "upstream_error")
	})
	t.Run("found", func(t *testing.T) {
		w := serveNais(&NaisApi{NaisClient: naisTeamStub{details: &nais.TeamDetails{Slug: "team", Members: []nais.TeamMember{}}}}, (*NaisApi).DetailsForTeam, "team")
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("expected 200 JSON, got %d %q", w.Code, w.Header().Get("Content-Type"))
		}
		if !strings.Contains(w.Body.String(), `"members":[]`) {
			t.Errorf("expected empty members array, got %s", w.Body.String())
		}
	})
}

func TestRepositoriesForTeam(t *testing.T) {
	for _, tt := range []struct {
		name   string
		nais   naisTeamStub
		gh     naisGitHubStub
		status int
		code   string
		body   string
	}{
		{"team not found", naisTeamStub{err: nais.ErrTeamNotFound}, naisGitHubStub{}, 404, "team_not_found", ""},
		{"no repositories", naisTeamStub{}, naisGitHubStub{}, 200, "", "[]\n"},
		{"no GitHub team", naisTeamStub{repos: []string{"navikt/a"}}, naisGitHubStub{err: &httpsupport.HttpError{Code: 404}}, 200, "", "[\"navikt/a\"]\n"},
		{"merged", naisTeamStub{repos: []string{"navikt/a"}}, naisGitHubStub{repos: []string{"navikt/a", "navikt/b"}}, 200, "", "[\"navikt/a\",\"navikt/b\"]\n"},
		{"GitHub error", naisTeamStub{}, naisGitHubStub{err: &httpsupport.HttpError{Code: 500}}, 502, "upstream_error", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := serveNais(&NaisApi{NaisClient: tt.nais, GitHubClient: tt.gh}, (*NaisApi).RepositoriesForTeam, "team")
			if tt.code != "" {
				assertProblem(t, w, tt.status, tt.code)
				return
			}
			if w.Code != tt.status || w.Body.String() != tt.body {
				t.Errorf("expected %d %q, got %d %q", tt.status, tt.body, w.Code, w.Body.String())
			}
		})
	}
}

func TestMergeUnique(t *testing.T) {
	tests := []struct {
		name     string
		a        []string
		b        []string
		expected []string
	}{
		{
			name:     "no overlap",
			a:        []string{"navikt/repo-a"},
			b:        []string{"navikt/repo-b"},
			expected: []string{"navikt/repo-a", "navikt/repo-b"},
		},
		{
			name:     "full overlap",
			a:        []string{"navikt/repo-a", "navikt/repo-b"},
			b:        []string{"navikt/repo-a", "navikt/repo-b"},
			expected: []string{"navikt/repo-a", "navikt/repo-b"},
		},
		{
			name:     "partial overlap",
			a:        []string{"navikt/repo-a", "navikt/repo-b"},
			b:        []string{"navikt/repo-b", "navikt/repo-c"},
			expected: []string{"navikt/repo-a", "navikt/repo-b", "navikt/repo-c"},
		},
		{
			name:     "empty slices",
			a:        nil,
			b:        nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := mergeUnique(tt.a, tt.b)
			if !reflect.DeepEqual(tt.expected, actual) {
				t.Errorf("expected %v, got %v", tt.expected, actual)
			}
		})
	}
}
