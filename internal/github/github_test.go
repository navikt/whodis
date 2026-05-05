package github

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSamlUsersResponseParsing(t *testing.T) {
	var response SamlUsersResponse
	_ = json.Unmarshal([]byte(samlUsersResponse), &response)
	expected := map[string]string{
		"ukjent":     "Ukjent.Person@nav.no",
		"utvikleren": "En.Utvikler@nav.no",
	}
	actual := response.AsMap()
	if !reflect.DeepEqual(expected, response.AsMap()) {
		t.Error("Expected", expected, "got", actual)
	}
}

var samlUsersResponse = `{
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
            }
          ]
        }
      }
    }
  }
}`
