package github

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
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

func TestParseNaisYaml(t *testing.T) {
	client := New("pkey", "cid", "aid")
	var frr fileReadResponse
	_ = json.Unmarshal([]byte(naisYamlContentsResponse), &frr)
	fileContents, _ := client.extractTextFrom(frr)
	var parsedNnaisYaml naisYaml
	_ = yaml.Unmarshal(fileContents, &parsedNnaisYaml)
	fmt.Println(parsedNnaisYaml.Metadata.Namespace)
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

var naisYamlContentsResponse = `{
  "name": "file-read-results.yaml",
  "path": ".nais/file-read-results.yaml",
  "sha": "8447e1e632dc43ab93e1b3abb6daf21ae5a9018a",
  "size": 1171,
  "url": "https://api.github.com/repos/navikt/whodis/contents/.nais/nais.yaml?ref=main",
  "html_url": "https://github.com/navikt/whodis/blob/main/.nais/nais.yaml",
  "git_url": "https://api.github.com/repos/navikt/whodis/git/blobs/8447e1e632dc43ab93e1b3abb6daf21ae5a9018a",
  "download_url": "https://raw.githubusercontent.com/navikt/whodis/main/.nais/nais.yaml",
  "type": "file",
  "content": "YXBpVmVyc2lvbjogbmFpcy5pby92MWFscGhhMQpraW5kOiBBcHBsaWNhdGlv\nbgptZXRhZGF0YToKICBuYW1lOiB3aG9kaXMKICBuYW1lc3BhY2U6IGFwcHNl\nYwogIGxhYmVsczoKICAgIHRlYW06IGFwcHNlYwpzcGVjOgogIGluZ3Jlc3Nl\nczoKICAgIC0gaHR0cHM6Ly93aG9kaXMuaW50ZXJuLm5hdi5ubwogIGltYWdl\nOiB7e2ltYWdlfX0KICBwb3J0OiA4MDgwCiAgbGl2ZW5lc3M6CiAgICBpbml0\naWFsRGVsYXk6IDYwCiAgICBwYXRoOiAvaW50ZXJuYWwvaXNhbGl2ZQogIHJl\nYWRpbmVzczoKICAgIGluaXRpYWxEZWxheTogNjAKICAgIHBhdGg6IC9pbnRl\ncm5hbC9pc3JlYWR5CiAgcmVwbGljYXM6CiAgICBtYXg6IDIKICAgIG1pbjog\nMgogIHJlc291cmNlczoKICAgIGxpbWl0czoKICAgICAgbWVtb3J5OiAzMDBN\naQogICAgcmVxdWVzdHM6CiAgICAgIGNwdTogMTAwbQogICAgICBtZW1vcnk6\nIDEwME1pCiAgb2JzZXJ2YWJpbGl0eToKICAgIGxvZ2dpbmc6CiAgICAgIGRl\nc3RpbmF0aW9uczoKICAgICAgICAtIGlkOiBsb2tpCiAgICBhdXRvSW5zdHJ1\nbWVudGF0aW9uOgogICAgICBlbmFibGVkOiB0cnVlCiAgICAgIHJ1bnRpbWU6\nIHNkawogIGF6dXJlOgogICAgYXBwbGljYXRpb246CiAgICAgIGVuYWJsZWQ6\nIHRydWUKICAgICAgYWxsb3dBbGxVc2VyczogZmFsc2UKICAgICAgY2xhaW1z\nOgogICAgICAgIGdyb3VwczoKICAgICAgICAgIC0gaWQ6IGQ2MzNkMjdiLWE0\nNzAtNGI5ZC05ODRlLTg1NDk5ZjEyNmUxOAogICAgc2lkZWNhcjoKICAgICAg\nYXV0b0xvZ2luOiB0cnVlCiAgICAgIGVuYWJsZWQ6IHRydWUKICBlbnY6CiAg\nICAtIG5hbWU6IFdFTExfS05PV05fVVJJCiAgICAgIHZhbHVlOiBodHRwczov\nL2xvZ2luLm1pY3Jvc29mdG9ubGluZS5jb20vbmF2Lm5vL3YyLjAvLndlbGwt\na25vd24vb3BlbmlkLWNvbmZpZ3VyYXRpb24KICAgIC0gbmFtZTogR0lOX01P\nREUKICAgICAgdmFsdWU6IHJlbGVhc2UKICBlbnZGcm9tOgogICAgLSBzZWNy\nZXQ6IHdob2RpcwogIGFjY2Vzc1BvbGljeToKICAgIG91dGJvdW5kOgogICAg\nICBydWxlczoKICAgICAgICAtIGFwcGxpY2F0aW9uOiB0ZWFtLWNhdGFsb2ct\nYmFja2VuZAogICAgICAgICAgbmFtZXNwYWNlOiBvcmcKICAgICAgZXh0ZXJu\nYWw6CiAgICAgICAgLSBob3N0OiBjb25zb2xlLm5hdi5jbG91ZC5uYWlzLmlv\nCg==\n",
  "encoding": "base64",
  "_links": {
    "self": "https://api.github.com/repos/navikt/whodis/contents/.nais/nais.yaml?ref=main",
    "git": "https://api.github.com/repos/navikt/whodis/git/blobs/8447e1e632dc43ab93e1b3abb6daf21ae5a9018a",
    "html": "https://github.com/navikt/whodis/blob/main/.nais/nais.yaml"
  }
}`
