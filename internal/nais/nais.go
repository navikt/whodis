package nais

import (
	"strings"

	"github.com/navikt/whodis/internal/httpsupport"
)

var naisApiBaseUrl = "https://console.nav.cloud.nais.io/graphql"

type Api struct {
	apiKey string
}

func New(apiKey string) *Api {
	return &Api{apiKey}
}

func (api *Api) DetailsFor(appName string) (string, error) {
	query := strings.Replace(applicationQuery, "$appName", appName, 1)
	resp, err := httpsupport.MakeGqlQuery[string](naisApiBaseUrl, api.apiKey, query)
	if err != nil {
		return "", err
	}
	return *resp, nil
}

var applicationQuery = `query app {
	application(name: "$appName") {
    	team {
			slug
			slackChannel
			members(first: 30) {
				user {
					email
				}
			}
		}
	}
} `
