package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewRepositoryID(t *testing.T) {
	tests := []struct {
		name  string
		owner string
		repo  string
		valid bool
	}{
		{"normal name", "navikt", "whodis", true},
		{"mixed case organization", "NaViKt", "Some.Repo", true},
		{"allowed punctuation", "navikt", ".github", true},
		{"underscores and hyphens", "navikt", "repo_name-1", true},
		{"maximum length", "navikt", strings.Repeat("a", 100), true},
		{"unsupported organization", "other", "whodis", false},
		{"missing organization", "", "whodis", false},
		{"malformed organization", "navikt/other", "whodis", false},
		{"empty name", "navikt", "", false},
		{"too long", "navikt", strings.Repeat("a", 101), false},
		{"dot segment", "navikt", ".", false},
		{"parent segment", "navikt", "..", false},
		{"slash", "navikt", "other/repo", false},
		{"encoded slash", "navikt", "other%2Frepo", false},
		{"query", "navikt", "repo?name=value", false},
		{"fragment", "navikt", "repo#fragment", false},
		{"space", "navikt", "repo name", false},
		{"backslash", "navikt", `other\repo`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository, err := NewRepositoryID(tt.owner, tt.repo)
			if (err == nil) != tt.valid {
				t.Fatalf("valid = %v, got error %v", tt.valid, err)
			}
			if tt.valid && repository.String() != "navikt/"+tt.repo {
				t.Errorf("unexpected repository identity %q", repository.String())
			}
		})
	}
}

func TestRepositoryTeamRequests(t *testing.T) {
	previousTransport := http.DefaultTransport
	http.DefaultTransport = repositoryTransport(func(r *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(r.URL.Path, "/repos/") {
			return nil, fmt.Errorf("unexpected GitHub request: %s", r.URL)
		}
		if r.Method != http.MethodGet || r.URL.String() != "https://api.github.com/repos/navikt/Some.Repo/teams" {
			t.Errorf("unexpected GitHub request: %s %s", r.Method, r.URL.RequestURI())
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing installation token")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`[{"slug":"admin-team","permission":"admin"},{"slug":"reader-team","permission":"pull"},{"slug":"ignored-team","permission":"admin"}]`)),
			Header:     make(http.Header),
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	client := &Client{
		installationToken:       "test-token",
		installationTokenExpiry: time.Now().Add(time.Hour),
		teamsToSkip:             []string{"ignored-team"},
	}

	repository, err := NewRepositoryID("NAVIKT", "Some.Repo")
	if err != nil {
		t.Fatal(err)
	}
	admins, err := client.AdminTeamsFor(repository, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(admins, []string{"admin-team"}) {
		t.Errorf("unexpected admin teams: %v", admins)
	}
	all, err := client.AllTeamsFor(repository, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(all, []string{"admin-team", "reader-team"}) {
		t.Errorf("unexpected teams: %v", all)
	}
}

type repositoryTransport func(*http.Request) (*http.Response, error)

func (f repositoryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
