package github

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
	client := New(string(data), "123", "321")
	token, err := client.createExchangeToken()
	if err != nil || token == "" {
		t.Fatalf("Error creating token: %v", err)
	}
}

func TestFilteringOutOrgOwnersFromRepoAdmins(t *testing.T) {
	client := Client{
		pkPEM:     "yolo",
		orgAdmins: []string{"orgadmin1", "orgadmin2"},
	}
	repoAdmins := []string{"repoadmin1", "repoadmin2", "orgadmin1", "orgadmin2"}
	expected := []string{"repoadmin1", "repoadmin2"}
	actual := client.filterOutOrgAdmins(repoAdmins)
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Org admins should be filtered out: %v", actual)
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
