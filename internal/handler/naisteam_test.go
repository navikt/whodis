package handler

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

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

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400 Bad Request, got %d", w.Code)
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

