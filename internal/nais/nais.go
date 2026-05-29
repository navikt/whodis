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

func (api *Api) DetailsFor(teamSlug string) (string, error) {
	query := strings.Replace(teamQuery, "$slug", teamSlug, 1)
	resp, err := httpsupport.MakeGqlQuery[string](naisApiBaseUrl, api.apiKey, query)
	if err != nil {
		return "", err
	}
	return *resp, nil
}

var teamQuery = `query singleTeam {
       team(slug:\"$slug\") {
          slug
          members(first:50 after:\"\") {
             pageInfo {
                totalCount
                hasNextPage
                endCursor
             }
             nodes {
                name
                email
             }
          }
       }
    }`
