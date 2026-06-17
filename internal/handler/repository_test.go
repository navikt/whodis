package handler

import (
	"reflect"
	"testing"

	"github.com/navikt/whodis/internal/teamkatalogen"
)

func TestExtractionOfUniqueTeamsAndSlackChannels(t *testing.T) {
	fromTeamkatalogen := []teamkatalogen.UserDetails{
		{
			Teams: []teamkatalogen.Team{
				{
					Name:         "team1",
					SlackChannel: "ch1",
					NaisTeams:    []string{"nais1", "nais2"},
				},
				{
					Name:         "team2",
					SlackChannel: "ch1",
					NaisTeams:    []string{"nais1", "nais2"},
				},
				{
					Name:         "team3",
					SlackChannel: "ch2",
					NaisTeams:    []string{"nais3", "nais4"},
				},
				{
					Name:         "team3",
					SlackChannel: "ch3",
					NaisTeams:    []string{"nais3", "nais4"},
				},
			},
		},
	}

	expected := &uniqueThings{
		Teams:         []string{"team1", "team2", "team3"},
		SlackChannels: []string{"ch1", "ch2", "ch3"},
	}
	actual := extractUnique(fromTeamkatalogen)
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("expected: %v, actual: %v", expected, actual)
	}
}
