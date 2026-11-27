package application

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRepositoryRoutesRejectInvalidIdentities(t *testing.T) {
	previousLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	app := &App{}
	app.loadRoutes()

	for _, endpoint := range []string{"owners", "slackchannels"} {
		for _, identity := range []string{
			"other/whodis",
			"navikt/repo%3Fquery",
			"navikt/repo%23fragment",
			"navikt/other%2Frepo",
			"navikt/.",
			"navikt/..",
			"navikt/" + strings.Repeat("a", 101),
			"repo%3Fquery",
			strings.Repeat("a", 101),
		} {
			path := "/repository/" + identity + "/" + endpoint
			t.Run(path, func(t *testing.T) {
				w := httptest.NewRecorder()
				app.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
				if w.Code != http.StatusBadRequest {
					t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
				}
				if got := w.Header().Get("Content-Type"); got != "application/problem+json" {
					t.Errorf("expected problem details, got %q", got)
				}
				var problem struct {
					Type   string `json:"type"`
					Title  string `json:"title"`
					Status int    `json:"status"`
					Detail string `json:"detail"`
					Code   string `json:"code"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
					t.Fatal(err)
				}
				if problem.Type != "about:blank" || problem.Title != "Bad Request" ||
					problem.Status != http.StatusBadRequest || problem.Detail == "" ||
					problem.Code != "invalid_repository" {
					t.Errorf("unexpected problem details: %+v", problem)
				}
			})
		}
	}
}

func TestLegacyRepositoryRoute(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/repository/Some.Repo/owners", nil)
	request.SetPathValue("repoName", "Some.Repo")
	called := false
	legacyRepositoryRoute(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.PathValue("owner") != "navikt" || r.PathValue("repoName") != "Some.Repo" {
			t.Errorf("unexpected legacy identity: %s/%s", r.PathValue("owner"), r.PathValue("repoName"))
		}
	})(httptest.NewRecorder(), request)
	if !called {
		t.Fatal("legacy adapter did not call the canonical handler")
	}
}

func TestRouteNotFound(t *testing.T) {
	previousLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	app := &App{}
	app.loadRoutes()

	for _, path := range []string{
		"/repository/example/ownerss",
		"/unknown",
		"/nais/example/repositories/unknown",
	} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			app.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

			if w.Code != http.StatusNotFound {
				t.Fatalf("expected 404, got %d", w.Code)
			}
			if got := w.Header().Get("Content-Type"); got != "application/problem+json" {
				t.Errorf("expected problem details content type, got %q", got)
			}
			var problem struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
				t.Fatal(err)
			}
			if problem.Code != "route_not_found" {
				t.Errorf("expected route_not_found, got %q", problem.Code)
			}
		})
	}

	t.Run("unsupported method remains 405", func(t *testing.T) {
		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/repository/example/owners", nil))
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", w.Code)
		}
	})
	t.Run("existing route remains available", func(t *testing.T) {
		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})
}

func TestEnvVarsArePutInTheCorrectConfigProp(t *testing.T) {
	_ = os.Setenv("GITHUB_APP_PRIVATE_KEY", "pk")
	_ = os.Setenv("GITHUB_APP_CLIENT_ID", "cid")
	_ = os.Setenv("GITHUB_APP_INSTALLATION_ID", "aid")
	_ = os.Setenv("NAIS_SERVICE_ACCOUNT_TOKEN_PATH", "thepath")
	_ = os.Setenv("GITHUB_TEAMS_TO_SKIP", "first,second")

	expected := &config{
		AppPrivateKeyPem:   "pk",
		AppClientID:        "cid",
		AppInstallationID:  "aid",
		NaisApiKeyLocation: "thepath",
		GitHubTeamsToSkip:  []string{"first", "second"},
	}
	actual, err := configFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Env vars end up in the wrong config prop: %v", actual)
	}
}
