package nais

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/navikt/whodis/internal/httpsupport"
)

func TestTeamsForRepository(t *testing.T) {
	tests := []struct {
		name      string
		responses []string
		want      []string
		wantErr   bool
	}{
		{
			name: "exact case-insensitive identities and multiple teams",
			responses: []string{`{"data":{"teams":{"nodes":[
				{"slug":"prefix-only","repositories":{"nodes":[{"name":"navikt/dp-meldekort-extra"}]}},
				{"slug":"one","repositories":{"nodes":[{"name":"NAVIKT/DP-MELDEKORT"}]}},
				{"slug":"two","repositories":{"nodes":[{"name":"navikt/dp-meldekort"}]}},
				{"slug":"one","repositories":{"nodes":[{"name":"navikt/dp-meldekort"}]}},
				{"slug":"other-org","repositories":{"nodes":[{"name":"other/dp-meldekort"}]}}
			],"pageInfo":{"hasNextPage":false}}}}`},
			want: []string{"one", "two"},
		},
		{
			name: "team pagination",
			responses: []string{
				`{"data":{"teams":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"next-team"}}}}`,
				`{"data":{"teams":{"nodes":[{"slug":"one","repositories":{"nodes":[{"name":"navikt/dp-meldekort"}]}}],"pageInfo":{"hasNextPage":false}}}}`,
			},
			want: []string{"one"},
		},
		{
			name: "repository pagination",
			responses: []string{
				`{"data":{"teams":{"nodes":[{"slug":"one","repositories":{"nodes":[{"name":"navikt/dp-meldekort-extra"}],"pageInfo":{"hasNextPage":true,"endCursor":"filtered-page"}}}],"pageInfo":{"hasNextPage":false}}}}`,
				`{"data":{"team":{"slug":"one","repositories":{"nodes":[{"name":"unrelated"}],"pageInfo":{"hasNextPage":true,"endCursor":"next-repo"}}}}}`,
				`{"data":{"team":{"slug":"one","repositories":{"nodes":[{"name":"navikt/dp-meldekort"}],"pageInfo":{"hasNextPage":false}}}}}`,
			},
			want: []string{"one"},
		},
		{
			name:      "no matches",
			responses: []string{`{"data":{"teams":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}`},
		},
		{
			name:      "GraphQL failure",
			responses: []string{`{"errors":[{"message":"access denied"}]}`},
			wantErr:   true,
		},
		{
			name: "failure on subsequent team page",
			responses: []string{
				`{"data":{"teams":{"nodes":[{"slug":"one","repositories":{"nodes":[{"name":"navikt/dp-meldekort"}]}}],"pageInfo":{"hasNextPage":true,"endCursor":"next-team"}}}}`,
				`{"errors":[{"message":"access denied"}]}`,
			},
			wantErr: true,
		},
		{
			name: "repository page failure",
			responses: []string{
				`{"data":{"teams":{"nodes":[{"slug":"one","repositories":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"filtered-page"}}}],"pageInfo":{"hasNextPage":false}}}}`,
				`{"errors":[{"message":"access denied"}]}`,
			},
			wantErr: true,
		},
		{
			name:      "missing team cursor",
			responses: []string{`{"data":{"teams":{"nodes":[],"pageInfo":{"hasNextPage":true}}}}`},
			wantErr:   true,
		},
		{
			name: "repeated team cursor",
			responses: []string{
				`{"data":{"teams":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"next-team"}}}}`,
				`{"data":{"teams":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"next-team"}}}}`,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			mockNaisHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("unexpected request method or authorization")
				}
				var body struct {
					Query string `json:"query"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("invalid GraphQL JSON: %v", err)
				}
				if strings.Contains(body.Query, "teams(first:") &&
					!strings.Contains(body.Query, `filter: {name: "navikt/dp-meldekort"}`) {
					t.Errorf("missing repository filter: %s", body.Query)
				}
				if calls == 1 && tt.name == "team pagination" && !strings.Contains(body.Query, `after: "next-team"`) {
					t.Errorf("missing team cursor: %s", body.Query)
				}
				if calls == 2 && tt.name == "repository pagination" && !strings.Contains(body.Query, `after: "next-repo"`) {
					t.Errorf("missing repository cursor: %s", body.Query)
				}
				if calls >= len(tt.responses) {
					t.Error("unexpected extra request")
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				response := tt.responses[calls]
				calls++
				_, _ = w.Write([]byte(response))
			})
			tokenPath := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(tokenPath, []byte("test-token"), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := New(tokenPath).TeamsForRepository("navikt/dp-meldekort", context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
			if calls != len(tt.responses) {
				t.Errorf("expected %d requests, got %d", len(tt.responses), calls)
			}
		})
	}
}

func TestTeamsForRepositoryFailures(t *testing.T) {
	t.Run("missing token", func(t *testing.T) {
		_, err := New(filepath.Join(t.TempDir(), "missing")).TeamsForRepository("navikt/dp-meldekort", context.Background())
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected missing token error, got %v", err)
		}
	})
	t.Run("HTTP failure", func(t *testing.T) {
		mockNaisHTTP(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
		tokenPath := filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(tokenPath, []byte("test-token"), 0600); err != nil {
			t.Fatal(err)
		}

		_, err := New(tokenPath).TeamsForRepository("navikt/dp-meldekort", context.Background())
		var httpErr *httpsupport.HttpError
		if !errors.As(err, &httpErr) || httpErr.Code != http.StatusForbidden {
			t.Fatalf("expected forbidden error, got %v", err)
		}
	})
}

type naisRoundTripper func(*http.Request) (*http.Response, error)

func (f naisRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func mockNaisHTTP(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = naisRoundTripper(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Result(), nil
	})
}
