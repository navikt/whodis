package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/httplog/v3"
)

type problemDetails struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
	Code   string `json:"code"`
}

func invalidRepository(w http.ResponseWriter, r *http.Request, err error) {
	writeProblem(w, r, problemDetails{
		Type:   "about:blank",
		Title:  "Invalid repository",
		Status: http.StatusBadRequest,
		Detail: err.Error(),
		Code:   "invalid_repository",
	}, "error writing invalid-repository response")
}

func RouteNotFound(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, problemDetails{
		Type:   "about:blank",
		Title:  "Route not found",
		Status: http.StatusNotFound,
		Detail: "No endpoint matches this request path.",
		Code:   "route_not_found",
	}, "error writing not-found response")
}

func writeProblem(w http.ResponseWriter, r *http.Request, problem problemDetails, logMessage string) {
	httplog.SetAttrs(r.Context(), slog.String("error.code", problem.Code))
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(problem.Status)
	if err := json.NewEncoder(w).Encode(problem); err != nil {
		slog.ErrorContext(r.Context(), logMessage, slog.Any("error", err))
	}
}
