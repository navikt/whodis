package handler

import (
	"reflect"
	"testing"

	"github.com/navikt/whodis/internal/teamkatalogen"
)

func TestExtractionOfUniqueSlackChannels(t *testing.T) {
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

	expected := []string{"ch1", "ch2", "ch3"}
	actual := extractUniqueSlackChannels(fromTeamkatalogen)
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("expected: %v, actual: %v", expected, actual)
	}
}

func TestSlackChannelsMayBeCommaSeparated(t *testing.T) {
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
					SlackChannel: "ch1, ch2, ch3",
					NaisTeams:    []string{"nais1", "nais2"},
				},
			},
		},
	}

	expected := []string{"ch1", "ch2", "ch3"}
	actual := extractUniqueSlackChannels(fromTeamkatalogen)
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("expected: %v, actual: %v", expected, actual)
	}
}

func TestSlackChannelsMayBeSpaceSeparated(t *testing.T) {
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
					SlackChannel: "ch1  ch2  ch3",
					NaisTeams:    []string{"nais1", "nais2"},
				},
			},
		},
	}

	expected := []string{"ch1", "ch2", "ch3"}
	actual := extractUniqueSlackChannels(fromTeamkatalogen)
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("expected: %v, actual: %v", expected, actual)
	}
}
