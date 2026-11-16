package handler

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/httplog/v3"
	"github.com/navikt/whodis/internal/httpsupport"
	"github.com/navikt/whodis/internal/teamkatalogen"
)

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
				handlePossible404(tt.err, w)
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
