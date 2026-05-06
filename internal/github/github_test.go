package github

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestAbilityToCreateJwtSignedWithSuppliedPEM(t *testing.T) {
	wd, _ := os.Getwd()
	path := filepath.Join(wd, "..", "..", "testfiles", "private_key.pem")
	data, _ := os.ReadFile(path)
	Init(string(data), "the_client", "")
	_, err := createExchangeToken()
	if err != nil {
		t.Fatalf("Error creating token: %v", err)
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
