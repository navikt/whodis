package github

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSamlUsersResponseParsing(t *testing.T) {
	var response samlUsersResponse
	_ = json.Unmarshal([]byte(samlUsersResponseWithNulls), &response)
	expected := map[string]string{
		"ukjent":     "Ukjent.Person@nav.no",
		"utvikleren": "En.Utvikler@nav.no",
	}
	actual := response.AsMap()
	if !reflect.DeepEqual(expected, response.AsMap()) {
		t.Error("Expected", expected, "got", actual)
	}
}

func TestAbilityToCreateJwtSignedWithSuppliedPEM(t *testing.T) {
	wd, _ := os.Getwd()
	path := filepath.Join(wd, "..", "..", "testfiles", "private_key.pem")
	data, _ := os.ReadFile(path)
	client := New(string(data), "123", "321", make([]string, 0))
	token, err := client.createExchangeToken()
	if err != nil || token == "" {
		t.Fatalf("Error creating token: %v", err)
	}
}

func TestFilteringOutOrgOwnersFromRepoAdmins(t *testing.T) {
	client := Client{
		orgAdmins: []string{"orgadmin1", "orgadmin2"},
	}
	repoAdmins := []string{"repoadmin1", "repoadmin2", "orgadmin1", "orgadmin2"}
	expected := []string{"repoadmin1", "repoadmin2"}
	actual := client.filterUnwanted(repoAdmins, client.orgAdmins)
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Org admins should be filtered out: %v", actual)
	}
}

func TestFilteringOutUnwantedTeams(t *testing.T) {
	client := Client{
		teamsToSkip: []string{"team2"},
	}
	allTeamsForRepo := []string{"team1", "team2", "team3"}
	expected := []string{"team1", "team3"}
	actual := client.filterUnwanted(allTeamsForRepo, client.teamsToSkip)
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Unwanted teams should be filtered out: %v", actual)
	}
}

func TestInstallationTokenExpiresInLessThan10Mins(t *testing.T) {
	now, _ := time.Parse(time.RFC1123Z, "Mon, 15 Jun 2026 23:15:10 +0200")
	expiry, _ := time.Parse(time.RFC1123Z, "Mon, 15 Jun 2026 23:25:09 +0200")
	c := Client{
		installationToken:       "whatever",
		installationTokenExpiry: expiry,
	}
	if !c.tokenShouldBeRefreshed(now) {
		t.Error("token should be refreshed")
	}
}

func TestInstallationTokenExpiresInMoreThan10Mins(t *testing.T) {
	now, _ := time.Parse(time.RFC1123Z, "Mon, 15 Jun 2026 23:15:10 +0200")
	expiry, _ := time.Parse(time.RFC1123Z, "Mon, 15 Jun 2026 23:25:11 +0200")
	c := Client{
		installationToken:       "whatever",
		installationTokenExpiry: expiry,
	}
	if c.tokenShouldBeRefreshed(now) {
		t.Error("token should not be refreshed")
	}
}

func TestInstallationTokenExpiresInLessThan10MinsDifferentTimeZones(t *testing.T) {
	now, _ := time.Parse(time.RFC1123Z, "Mon, 15 Jun 2026 23:15:10 +0200")
	expiry, _ := time.Parse(time.RFC3339, "2026-06-15T21:25:09Z")
	c := Client{
		installationToken:       "whatever",
		installationTokenExpiry: expiry,
	}
	if !c.tokenShouldBeRefreshed(now) {
		t.Error("token should be refreshed")
	}
}

func TestMissingInstallationTokenMustBeRefreshed(t *testing.T) {
	now, _ := time.Parse(time.RFC1123Z, "Mon, 15 Jun 2026 23:15:10 +0200")
	expiry, _ := time.Parse(time.RFC3339, "2026-06-15T21:25:11Z")
	c := Client{
		installationTokenExpiry: expiry,
	}
	if !c.tokenShouldBeRefreshed(now) {
		t.Error("token should be refreshed")
	}
}

var samlUsersResponseWithNulls = `{
  "data": {
    "organization": {
      "samlIdentityProvider": {
        "externalIdentities": {
          "pageInfo": {
            "hasNextPage": true,
            "endCursor": "Y3Vyc29yOnYyOpHNV8k="
          },
          "edges": [
            {
              "node": {
                "samlIdentity": {
                  "emails": [
                    {
                      "value": "Ukjent.Person@nav.no"
                    }
                  ]
                },
                "user": {
                  "login": "ukjent"
                }
              }
            },
            {
              "node": {
                "samlIdentity": {
                  "emails": [
                    {
                      "value": "En.Utvikler@nav.no"
                    }
                  ]
                },
                "user": {
                  "login": "utvikleren"
                }
              }
            },
			{
				"node":
					{"samlIdentity":{
						"emails":[]},
						"user":null
				}
			}
          ]
        }
      }
    }
  }
}`
