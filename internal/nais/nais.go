package nais

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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

// TeamsForRepository reverses the team repository connection exposed by Nais.
func (api *Api) TeamsForRepository(repository string, ctx context.Context) ([]string, error) {
	token, err := api.loadNaisApiToken()
	if err != nil {
		return nil, err
	}
	var teams []string
	var cursor string
	for {
		afterArg := ""
		if cursor != "" {
			afterArg = fmt.Sprintf(", after: %q", cursor)
		}
		query := fmt.Sprintf(`query {
				teams(first: 100%s) {
					nodes {
						slug
						repositories(first: 100, filter: {name: %q}) {
							nodes { name }
							pageInfo { hasNextPage endCursor }
						}
					}
					pageInfo { hasNextPage endCursor }
				}
			}`, afterArg, repository)
		// MakeGqlQuery expects quotes escaped for its JSON request body.
		query = strconv.Quote(query)
		query = query[1 : len(query)-1]
		resp, err := httpsupport.MakeGqlQuery[repositoryTeamsResponse](naisApiBaseUrl, token, query)
		if err != nil {
			return nil, err
		}
		for _, team := range resp.Data.Teams.Nodes {
			var names []string
			for _, node := range team.Repositories.Nodes {
				names = append(names, node.Name)
			}
			if team.Repositories.PageInfo.HasNextPage {
				names, err = api.RepositoriesFor(team.Slug, ctx)
				if err != nil {
					return nil, err
				}
			}
			for _, name := range names {
				// Nais filters by substring; ownership requires the full identity.
				if strings.EqualFold(name, repository) && !slices.Contains(teams, team.Slug) {
					teams = append(teams, team.Slug)
					break
				}
			}
		}
		if !resp.Data.Teams.PageInfo.HasNextPage {
			return teams, nil
		}
		if resp.Data.Teams.PageInfo.EndCursor == "" || resp.Data.Teams.PageInfo.EndCursor == cursor {
			return nil, errors.New("nais team pagination did not advance")
		}
		cursor = resp.Data.Teams.PageInfo.EndCursor
	}
}

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type repositoryTeamsResponse struct {
	Data struct {
		Teams struct {
			Nodes []struct {
				Slug         string `json:"slug"`
				Repositories struct {
					Nodes []struct {
						Name string `json:"name"`
					} `json:"nodes"`
					PageInfo pageInfo `json:"pageInfo"`
				} `json:"repositories"`
			} `json:"nodes"`
			PageInfo pageInfo `json:"pageInfo"`
		} `json:"teams"`
	} `json:"data"`
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
	for _, seg := range strings.Split(filepath.ToSlash(api.apiKeyLocation), "/") {
		if seg == ".." {
			return "", fmt.Errorf("invalid file path")
		}
	}
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
