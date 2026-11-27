package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/httplog/v3"
)

func assertProblem(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("expected status %d, got %d: %s", status, w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("expected problem details content type, got %q", got)
	}
	var problem problemDetails
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if problem.Type != "about:blank" || problem.Title != http.StatusText(status) ||
		problem.Status != status || problem.Code != code || problem.Detail == "" || problem.Instance == "" {
		t.Errorf("unexpected problem details: %+v", problem)
	}
}

func TestRouteNotFound(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := httplog.RequestLogger(logger, &httplog.Options{
		Level:  slog.LevelInfo,
		Schema: httplog.SchemaOTEL,
	})(http.HandlerFunc(RouteNotFound))
	req := httptest.NewRequest(http.MethodGet, "/repository/example/ownerss", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assertProblem(t, w, http.StatusNotFound, "route_not_found")
	if lines := strings.Split(strings.TrimSpace(logs.String()), "\n"); len(lines) != 1 {
		t.Fatalf("expected one request log, got %d: %s", len(lines), logs.String())
	}
	var entry struct {
		Code string `json:"error.code"`
		Path string `json:"url.path"`
	}
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Code != "route_not_found" || entry.Path != req.URL.Path {
		t.Errorf("unexpected request log: %+v", entry)
	}
}

func TestWriteJSONEncodingFailureReturnsProblem(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, httptest.NewRequest(http.MethodGet, "/x", nil), map[string]any{"bad": func() {}})
	assertProblem(t, w, http.StatusInternalServerError, "internal_error")
}

func TestDependencyFailureClassifiesNetworkErrorsAsBadGateway(t *testing.T) {
	w := httptest.NewRecorder()
	dependencyFailure(w, httptest.NewRequest(http.MethodGet, "/x", nil), "lookup failed",
		&net.OpError{Op: "dial", Err: errors.New("connection refused")})
	assertProblem(t, w, http.StatusBadGateway, "upstream_error")
}
