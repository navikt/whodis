package teamkatalogen

import (
	"context"
	"encoding/json"

	"github.com/navikt/whodis/internal/httpsupport"
	"go.opentelemetry.io/otel/trace"
)

var apiBaseURI = "http://team-catalog-backend.org"

func DetailsForUser(email string, ctx context.Context) (*UserDetails, error) {
	span := trace.SpanFromContext(ctx)
	defer span.End()
	resp, err := httpsupport.MakeUnauthenticatedGetRequest(apiBaseURI + "/member/membership/byUserEmail?email=" + email)
	if err != nil {
		return nil, err
	}
	var userDetalis UserDetails
	if err := json.Unmarshal(resp, &userDetalis); err != nil {
		return nil, err
	}
	return &userDetalis, nil
}

type UserDetails struct {
	Teams []Team `json:"teams"`
}

type Team struct {
	Id           string   `json:"id"`
	Name         string   `json:"name"`
	SlackChannel string   `json:"slackChannel"`
	NaisTeams    []string `json:"naisTeams"`
}
