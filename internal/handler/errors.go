package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"

	"github.com/go-chi/httplog/v3"
	"github.com/navikt/whodis/internal/httpsupport"
)

// problemDetails is an RFC 9457 problem object. Type is always about:blank, so
// Title is the HTTP status phrase and Code is a machine-readable extension.
type problemDetails struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance"`
	Code     string `json:"code"`
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	httplog.SetAttrs(r.Context(), slog.String("error.code", code))
	body, err := json.Marshal(problemDetails{
		Type:     "about:blank",
		Title:    http.StatusText(status),
		Status:   status,
		Detail:   detail,
		Instance: r.URL.Path,
		Code:     code,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "error encoding problem details", slog.Any("error", err))
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if _, err := w.Write(append(body, '\n')); err != nil {
		slog.ErrorContext(r.Context(), "error writing problem details", slog.Any("error", err))
	}
}

// writeJSON encodes before writing headers so encoding failures can still produce a 500.
func writeJSON(w http.ResponseWriter, r *http.Request, value any) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(value); err != nil {
		internalError(w, r, "error encoding response", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body.Bytes()); err != nil {
		slog.ErrorContext(r.Context(), "error writing response", slog.Any("error", err))
	}
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func notFound(w http.ResponseWriter, r *http.Request, code, detail string) {
	writeProblem(w, r, http.StatusNotFound, code, detail)
}

func RouteNotFound(w http.ResponseWriter, r *http.Request) {
	notFound(w, r, "route_not_found", "No endpoint matches this request path.")
}

func invalidRepository(w http.ResponseWriter, r *http.Request, err error) {
	writeProblem(w, r, http.StatusBadRequest, "invalid_repository", err.Error())
}

func invalidTeamSlug(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, http.StatusBadRequest, "invalid_team_slug",
		"Team slug must contain 1 to 50 letters, digits, hyphens or underscores.")
}

func internalError(w http.ResponseWriter, r *http.Request, logMessage string, err error) {
	slog.ErrorContext(r.Context(), logMessage, slog.Any("error", err))
	writeProblem(w, r, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.")
}

// dependencyFailure responds 504 for upstream timeouts, 502 for other upstream
// failures and 500 for errors that do not originate from a dependency.
func dependencyFailure(w http.ResponseWriter, r *http.Request, logMessage string, err error) {
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		slog.ErrorContext(r.Context(), logMessage, slog.Any("error", err))
		writeProblem(w, r, http.StatusGatewayTimeout, "upstream_timeout", "An upstream service did not respond in time.")
		return
	}
	var httpErr *httpsupport.HttpError
	var gqlErr *httpsupport.GqlError
	if errors.As(err, &httpErr) || errors.As(err, &gqlErr) || errors.As(err, &netErr) {
		slog.ErrorContext(r.Context(), logMessage, slog.Any("error", err))
		writeProblem(w, r, http.StatusBadGateway, "upstream_error", "An upstream service returned an error.")
		return
	}
	internalError(w, r, logMessage, err)
}

func isHTTPNotFound(err error) bool {
	var httpErr *httpsupport.HttpError
	return errors.As(err, &httpErr) && httpErr.Code == http.StatusNotFound
}
