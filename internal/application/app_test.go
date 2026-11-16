package application

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

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
