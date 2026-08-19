package nais

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/navikt/whodis/internal/httpsupport"
	"go.opentelemetry.io/otel/trace"
)

var ErrTeamNotFound = errors.New("team not found")

var naisApiBaseUrl = "https://console.nav.cloud.nais.io/graphql"

type Api struct {
	apiKeyLocation string
}

func New(apiKeyLocation string) *Api {
	return &Api{apiKeyLocation}
}

func (api *Api) DetailsFor(teamSlug string, ctx context.Context) (*TeamDetails, error) {
	span := trace.SpanFromContext(ctx)
	defer span.End()
	query := strings.Replace(teamQuery, "$slug", teamSlug, 1)
	token, err := api.loadNaisApiToken()
	if err != nil {
		return nil, err
	}
	resp, err := httpsupport.MakeGqlQuery[teamQueryResponse](naisApiBaseUrl, token, query)
	if err != nil {
		return nil, err
	}
	return resp.asTeam(), nil
}

func (api *Api) RepositoriesFor(teamSlug string, ctx context.Context) ([]string, error) {
	span := trace.SpanFromContext(ctx)
	defer span.End()
	token, err := api.loadNaisApiToken()
	if err != nil {
		return nil, err
	}

	var repos []string
	var cursor string
	for {
		query := buildRepoQuery(teamSlug, cursor)
		resp, err := httpsupport.MakeGqlQuery[repoQueryResponse](naisApiBaseUrl, token, query)
		if err != nil {
			var gqlErr *httpsupport.GqlError
			if errors.As(err, &gqlErr) && strings.Contains(strings.ToLower(gqlErr.Message), "not found") {
				return nil, ErrTeamNotFound
			}
			return nil, err
		}
		teamData := resp.Data.Team
		if teamData.Slug == "" {
			return nil, nil
		}
		for _, node := range teamData.Repositories.Nodes {
			repos = append(repos, node.Name)
		}
		pageInfo := teamData.Repositories.PageInfo
		if !pageInfo.HasNextPage {
			break
		}
		cursor = pageInfo.EndCursor
	}
	return repos, nil
}

func buildRepoQuery(teamSlug, cursor string) string {
	afterArg := ""
	if cursor != "" {
		afterArg = `, after: \"` + cursor + `\"`
	}
	return `query {
       team(slug:\"` + teamSlug + `\") {
          slug
          repositories(first: 100` + afterArg + `) {
             nodes { name }
             pageInfo { hasNextPage endCursor }
          }
       }
    }`
}

type repoQueryResponse struct {
	Data struct {
		Team struct {
			Slug         string `json:"slug"`
			Repositories struct {
				Nodes []struct {
					Name string `json:"name"`
				} `json:"nodes"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"repositories"`
		} `json:"team"`
	} `json:"data"`
}

func (api *Api) loadNaisApiToken() (string, error) {
	fileContents, err := os.ReadFile(api.apiKeyLocation)
	if err != nil {
		return "", err
	}
	return string(fileContents), nil
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
