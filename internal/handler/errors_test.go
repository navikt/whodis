package handler

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/httplog/v3"
)

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

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("expected problem details content type, got %q", got)
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
	if problem.Type != "about:blank" || problem.Title != "Route not found" ||
		problem.Status != http.StatusNotFound || problem.Code != "route_not_found" ||
		problem.Detail != "No endpoint matches this request path." {
		t.Errorf("unexpected problem details: %+v", problem)
	}
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
	if entry.Code != problem.Code || entry.Path != req.URL.Path {
		t.Errorf("unexpected request log: %+v", entry)
	}
}
