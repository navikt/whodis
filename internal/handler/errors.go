package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/httplog/v3"
)

func RouteNotFound(w http.ResponseWriter, r *http.Request) {
	httplog.SetAttrs(r.Context(), slog.String("error.code", "route_not_found"))
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusNotFound)
	problem := struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
		Code   string `json:"code"`
	}{
		Type:   "about:blank",
		Title:  "Route not found",
		Status: http.StatusNotFound,
		Detail: "No endpoint matches this request path.",
		Code:   "route_not_found",
	}
	if err := json.NewEncoder(w).Encode(problem); err != nil {
		slog.ErrorContext(r.Context(), "error writing not-found response", slog.Any("error", err))
	}
}
