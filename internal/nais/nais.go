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

func (api *Api) DetailsFor(teamSlug string) (*TeamDetails, error) {
	query := strings.Replace(teamQuery, "$slug", teamSlug, 1)
	resp, err := httpsupport.MakeGqlQuery[teamQueryResponse](naisApiBaseUrl, api.apiKey, query)
	if err != nil {
		return nil, err
	}
	return resp.asTeam(), nil
}

var teamQuery = `query {
       team(slug:\"$slug\") {
          slug
          slackChannel
          purpose
          members(first:50) {
             nodes {
                user {
                   email
                   name
                }
                role
             }
          }
       }
    }`

type teamQueryResponse struct {
	Data struct {
		Team struct {
			Slug         string `json:"slug"`
			SlackChannel string `json:"slackChannel"`
			Purpose      string `json:"purpose"`
			Members      struct {
				Nodes []struct {
					User struct {
						Email string `json:"email"`
						Name  string `json:"name"`
					}
					Role string `json:"role"`
				}
			}
		}
	}
}

func (tqr *teamQueryResponse) asTeam() *TeamDetails {
	var members []TeamMember
	for _, member := range tqr.Data.Team.Members.Nodes {
		members = append(members, TeamMember{
			Email: member.User.Email,
			Name:  member.User.Name,
			Role:  member.Role,
		})
	}
	return &TeamDetails{
		Slug:         tqr.Data.Team.Slug,
		SlackChannel: tqr.Data.Team.SlackChannel,
		Purpose:      tqr.Data.Team.Purpose,
		Members:      members,
	}
}

type TeamDetails struct {
	Slug         string       `json:"slug"`
	SlackChannel string       `json:"slackChannel"`
	Purpose      string       `json:"purpose"`
	Members      []TeamMember `json:"members"`
}

type TeamMember struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}
