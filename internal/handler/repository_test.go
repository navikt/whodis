package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/httplog/v3"
	"github.com/navikt/whodis/internal/github"
	"github.com/navikt/whodis/internal/httpsupport"
	"github.com/navikt/whodis/internal/nais"
	"github.com/navikt/whodis/internal/teamkatalogen"
	"go.opentelemetry.io/otel/trace/noop"
)

type repositoryGitHubStub struct {
	admins   []string
	teams    []string
	err      error
	requests []string
	allCalls int
}

func (c *repositoryGitHubStub) EmailFor(string) string { return "" }

func (c *repositoryGitHubStub) AdminTeamsFor(repository github.RepositoryID, _ context.Context) ([]string, error) {
	c.requests = append(c.requests, repository.String())
	return c.admins, c.err
}

func (c *repositoryGitHubStub) AllTeamsFor(repository github.RepositoryID, _ context.Context) ([]string, error) {
	c.requests = append(c.requests, repository.String())
	c.allCalls++
	return c.teams, c.err
}

type repositoryNaisStub struct {
	teams        []string
	err          error
	repositories *[]string
	details      map[string]*nais.TeamDetails
	detailErrors map[string]error
}

func (c repositoryNaisStub) TeamsForRepository(repository string, _ context.Context) ([]string, error) {
	if c.repositories != nil {
		*c.repositories = append(*c.repositories, repository)
	}
	return c.teams, c.err
}

func (c repositoryNaisStub) DetailsFor(team string, _ context.Context) (*nais.TeamDetails, error) {
	if err := c.detailErrors[team]; err != nil {
		return nil, err
	}
	if details, ok := c.details[team]; ok {
		return details, nil
	}
	return &nais.TeamDetails{SlackChannel: "#team"}, nil
}

func TestSlackNaisFallbackAfterUnresolvedGitHubTeams(t *testing.T) {
	for _, tt := range []struct {
		name        string
		details     map[string]*nais.TeamDetails
		errors      map[string]error
		status      int
		want        []string
		wantLookups int
	}{
		{
			name: "empty GitHub team channel",
			details: map[string]*nais.TeamDetails{
				"github-team": {SlackChannel: " "},
			},
			status: 200, want: []string{"#team"}, wantLookups: 1,
		},
		{
			name: "GitHub team missing in Nais",
			errors: map[string]error{
				"github-team": errors.New("specified team was not found"),
			},
			status: 200, want: []string{"#team"}, wantLookups: 1,
		},
		{
			name: "Nais team has no channel",
			details: map[string]*nais.TeamDetails{
				"github-team": {SlackChannel: ""},
				"nais-team":   {SlackChannel: ""},
			},
			status: 404, wantLookups: 1,
		},
		{
			name: "GitHub team details failure",
			errors: map[string]error{
				"github-team": errors.New("Nais unavailable"),
			},
			status: 500,
		},
		{
			name: "fallback team details failure",
			details: map[string]*nais.TeamDetails{
				"github-team": {SlackChannel: ""},
			},
			errors: map[string]error{
				"nais-team": errors.New("Nais unavailable"),
			},
			status: 500, wantLookups: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var lookups []string
			handler := &Repository{
				GitHubClient: &repositoryGitHubStub{teams: []string{"github-team"}},
				NaisClient: repositoryNaisStub{
					teams: []string{"nais-team"}, repositories: &lookups,
					details: tt.details, detailErrors: tt.errors,
				},
				Tracer: noop.NewTracerProvider().Tracer("test"),
			}
			request := httptest.NewRequest(http.MethodGet, "/repository/navikt/dp-meldekort/slackchannels", nil)
			request.SetPathValue("owner", "navikt")
			request.SetPathValue("repoName", "dp-meldekort")
			w := httptest.NewRecorder()
			handler.SlackChannelsForRepo(w, request)
			if w.Code != tt.status {
				t.Fatalf("expected status %d, got %d: %s", tt.status, w.Code, w.Body.String())
			}
			if len(lookups) != tt.wantLookups {
				t.Errorf("expected %d Nais ownership lookups, got %d", tt.wantLookups, len(lookups))
			}
			if tt.status == http.StatusOK {
				var got []string
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("expected %v, got %v", tt.want, got)
				}
			}
		})
	}
}

func TestRepositoryNaisFallback(t *testing.T) {
	for _, tt := range []struct {
		name        string
		endpoint    string
		admins      []string
		githubTeams []string
		naisTeams   []string
		naisErr     error
		status      int
		want        []string
		naisCalls   int
	}{
		{"Nais owners", "owners", nil, nil, []string{"team-one", "team-two"}, nil, 200, []string{"team-one", "team-two"}, 1},
		{"Nais Slack channels", "slackchannels", nil, nil, []string{"team-one", "team-two"}, nil, 200, []string{"#team"}, 1},
		{"GitHub admins take precedence", "owners", []string{"admin"}, nil, nil, errors.New("must not call Nais"), 200, []string{"admin"}, 0},
		{"GitHub teams take precedence", "owners", nil, []string{"reader"}, nil, errors.New("must not call Nais"), 200, []string{"reader"}, 0},
		{"GitHub channels take precedence", "slackchannels", nil, []string{"reader"}, nil, errors.New("must not call Nais"), 200, []string{"#team"}, 0},
		{"no Nais owners", "owners", nil, nil, nil, nil, 200, nil, 1},
		{"no Nais channels", "slackchannels", nil, nil, nil, nil, 404, nil, 1},
		{"Nais owner failure", "owners", nil, nil, nil, errors.New("Nais unavailable"), 500, nil, 1},
		{"Nais Slack failure", "slackchannels", nil, nil, nil, errors.New("Nais unavailable"), 500, nil, 1},
		{"Nais upstream 404 is not a missing repository", "owners", nil, nil, nil, &httpsupport.HttpError{Code: 404}, 500, nil, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var lookups []string
			handler := &Repository{
				GitHubClient: &repositoryGitHubStub{admins: tt.admins, teams: tt.githubTeams},
				NaisClient: repositoryNaisStub{
					teams: tt.naisTeams, err: tt.naisErr, repositories: &lookups,
				},
				Tracer: noop.NewTracerProvider().Tracer("test"),
			}
			request := httptest.NewRequest(http.MethodGet, "/repository/navikt/dp-meldekort/"+tt.endpoint, nil)
			request.SetPathValue("owner", "navikt")
			request.SetPathValue("repoName", "dp-meldekort")
			w := httptest.NewRecorder()
			if tt.endpoint == "owners" {
				handler.OwnerTeamsForRepo(w, request)
			} else {
				handler.SlackChannelsForRepo(w, request)
			}
			if w.Code != tt.status {
				t.Fatalf("expected status %d, got %d: %s", tt.status, w.Code, w.Body.String())
			}
			if tt.status == http.StatusOK {
				var got []string
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("expected %v, got %v", tt.want, got)
				}
			}
			if len(lookups) != tt.naisCalls {
				t.Fatalf("expected %d Nais lookups, got %v", tt.naisCalls, lookups)
			}
			for _, repository := range lookups {
				if repository != "navikt/dp-meldekort" {
					t.Errorf("unexpected repository %q", repository)
				}
			}
		})
	}
}

func TestRepositoryResolution(t *testing.T) {
	for _, tt := range []struct {
		name     string
		endpoint string
		admins   []string
		teams    []string
		err      error
		status   int
		want     []string
		allCalls int
	}{
		{"admin owners", "owners", []string{"admin"}, []string{"reader"}, nil, 200, []string{"admin"}, 0},
		{"all teams fallback", "owners", nil, []string{"reader"}, nil, 200, []string{"reader"}, 1},
		{"no owners", "owners", nil, nil, nil, 200, nil, 1},
		{"Slack channels", "slackchannels", nil, []string{"one", "two"}, nil, 200, []string{"#team"}, 1},
		{"no Slack channels", "slackchannels", nil, nil, nil, 404, nil, 1},
		{"owners missing", "owners", nil, nil, &httpsupport.HttpError{Code: 404}, 404, nil, 0},
		{"Slack repository missing", "slackchannels", nil, nil, &httpsupport.HttpError{Code: 404}, 404, nil, 1},
	} {
		for _, owner := range []string{"navikt", "NAVIKT"} {
			t.Run(tt.name+"/"+owner, func(t *testing.T) {
				client := &repositoryGitHubStub{admins: tt.admins, teams: tt.teams, err: tt.err}
				handler := &Repository{
					GitHubClient: client,
					NaisClient:   repositoryNaisStub{},
					Tracer:       noop.NewTracerProvider().Tracer("test"),
				}
				request := httptest.NewRequest(http.MethodGet, "/repository/"+owner+"/Some.Repo/"+tt.endpoint, nil)
				request.SetPathValue("owner", owner)
				request.SetPathValue("repoName", "Some.Repo")
				w := httptest.NewRecorder()
				if tt.endpoint == "owners" {
					handler.OwnerTeamsForRepo(w, request)
				} else {
					handler.SlackChannelsForRepo(w, request)
				}
				if w.Code != tt.status {
					t.Fatalf("expected status %d, got %d: %s", tt.status, w.Code, w.Body.String())
				}
				if tt.status == http.StatusOK {
					var got []string
					if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, tt.want) {
						t.Errorf("expected %v, got %v", tt.want, got)
					}
				} else if w.Body.Len() != 0 {
					t.Errorf("expected empty not-found response, got %s", w.Body.String())
				}
				if len(client.requests) == 0 {
					t.Fatal("expected a GitHub lookup")
				}
				for _, identity := range client.requests {
					if identity != "navikt/Some.Repo" {
						t.Errorf("unexpected repository identity %q", identity)
					}
				}
				if client.allCalls != tt.allCalls {
					t.Errorf("expected %d all-team lookups, got %d", tt.allCalls, client.allCalls)
				}
			})
		}
	}
}

func TestHandlePossible404(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantLogs   int
	}{
		{
			name:       "repository not found",
			err:        &httpsupport.HttpError{Code: http.StatusNotFound},
			wantStatus: http.StatusNotFound,
			wantLogs:   1,
		},
		{
			name:       "wrapped repository not found",
			err:        fmt.Errorf("getting repository teams: %w", &httpsupport.HttpError{Code: http.StatusNotFound}),
			wantStatus: http.StatusNotFound,
			wantLogs:   1,
		},
		{
			name:       "GitHub forbidden",
			err:        &httpsupport.HttpError{Code: http.StatusForbidden},
			wantStatus: http.StatusInternalServerError,
			wantLogs:   2,
		},
		{
			name:       "GitHub server error",
			err:        &httpsupport.HttpError{Code: http.StatusInternalServerError},
			wantStatus: http.StatusInternalServerError,
			wantLogs:   2,
		},
		{
			name:       "unexpected error",
			err:        errors.New("connection failed"),
			wantStatus: http.StatusInternalServerError,
			wantLogs:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			previousLogger := slog.Default()
			slog.SetDefault(logger)
			t.Cleanup(func() { slog.SetDefault(previousLogger) })

			handler := httplog.RequestLogger(logger, &httplog.Options{
				Level:  slog.LevelInfo,
				Schema: httplog.SchemaOTEL,
			})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlePossible404(tt.err, w, r)
			}))
			req := httptest.NewRequest(http.MethodGet, "/repository/deleted-repo/owners", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, w.Code)
			}
			lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
			if len(lines) != tt.wantLogs {
				t.Errorf("expected %d log entries, got %d: %s", tt.wantLogs, len(lines), logs.String())
			}
			if !strings.Contains(logs.String(), fmt.Sprintf("GET /repository/deleted-repo/owners => HTTP %d", tt.wantStatus)) {
				t.Errorf("expected request log with repository path and status %d: %s", tt.wantStatus, logs.String())
			}
			if tt.wantStatus == http.StatusInternalServerError && !strings.Contains(logs.String(), tt.err.Error()) {
				t.Errorf("expected unexpected error to be logged: %s", logs.String())
			}
			if tt.wantStatus == http.StatusNotFound {
				if w.Body.Len() != 0 {
					t.Errorf("expected empty not-found response, got %q", w.Body.String())
				}
				if got := w.Header().Get("Content-Type"); got == "application/problem+json" {
					t.Errorf("unexpected problem details content type on empty response")
				}
				var entry struct {
					Code string `json:"error.code"`
				}
				if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
					t.Fatal(err)
				}
				if entry.Code != "resource_not_found" {
					t.Errorf("expected log code resource_not_found, got %q", entry.Code)
				}
			} else if strings.Contains(logs.String(), `"error.code"`) {
				t.Errorf("unexpected not-found classification for server error: %s", logs.String())
			}
		})
	}
}

func TestExtractionOfUniqueSlackChannels(t *testing.T) {
	fromTeamkatalogen := []teamkatalogen.UserDetails{
		{
			Teams: []teamkatalogen.Team{
				{
					Name:         "team1",
					SlackChannel: "ch1",
					NaisTeams:    []string{"nais1", "nais2"},
				},
				{
					Name:         "team2",
					SlackChannel: "ch1",
					NaisTeams:    []string{"nais1", "nais2"},
				},
				{
					Name:         "team3",
					SlackChannel: "ch2",
					NaisTeams:    []string{"nais3", "nais4"},
				},
				{
					Name:         "team3",
					SlackChannel: "ch3",
					NaisTeams:    []string{"nais3", "nais4"},
				},
			},
		},
	}

	expected := []string{"ch1", "ch2", "ch3"}
	actual := extractUniqueSlackChannels(fromTeamkatalogen)
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("expected: %v, actual: %v", expected, actual)
	}
}

func TestSlackChannelsMayBeCommaSeparated(t *testing.T) {
	fromTeamkatalogen := []teamkatalogen.UserDetails{
		{
			Teams: []teamkatalogen.Team{
				{
					Name:         "team1",
					SlackChannel: "ch1",
					NaisTeams:    []string{"nais1", "nais2"},
				},
				{
					Name:         "team2",
					SlackChannel: "ch1, ch2, ch3",
					NaisTeams:    []string{"nais1", "nais2"},
				},
			},
		},
	}

	expected := []string{"ch1", "ch2", "ch3"}
	actual := extractUniqueSlackChannels(fromTeamkatalogen)
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("expected: %v, actual: %v", expected, actual)
	}
}

func TestSlackChannelsMayBeSpaceSeparated(t *testing.T) {
	fromTeamkatalogen := []teamkatalogen.UserDetails{
		{
			Teams: []teamkatalogen.Team{
				{
					Name:         "team1",
					SlackChannel: "ch1",
					NaisTeams:    []string{"nais1", "nais2"},
				},
				{
					Name:         "team2",
					SlackChannel: "ch1  ch2  ch3",
					NaisTeams:    []string{"nais1", "nais2"},
				},
			},
		},
	}

	expected := []string{"ch1", "ch2", "ch3"}
	actual := extractUniqueSlackChannels(fromTeamkatalogen)
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("expected: %v, actual: %v", expected, actual)
	}
}
