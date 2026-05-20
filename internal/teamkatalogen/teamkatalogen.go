package teamkatalogen

import (
	"encoding/json"

	"github.com/navikt/whodis/internal/httpsupport"
)

var apibaseURI = "https://teamkatalog-api.intern.nav.no"

type membershipByEmailReply struct {
	Teams []struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		SlackChannel string `json:"slackChannel"`
	}
}

func DetailsForUser(email string) (*UserDetails, error) {
	resp, err := httpsupport.MakeUnauthenticatedGetRequest(apibaseURI + "/member/membership/byUserEmail?email=" + email)
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
	Department   string   `json:"avdelingNavn"`
	Name         string   `json:"name"`
	SlackChannel string   `json:"slackChannel"`
	NaisTeams    []string `json:"naisTeams"`
}
