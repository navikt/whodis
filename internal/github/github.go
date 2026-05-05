package github

import (
	"maps"
	"strconv"
	"strings"

	"github.com/navikt/whodis/internal/httpsupport"
)

var authToken string

func Init(apiToken string) {
	authToken = apiToken
}

func AllUsers(authToken string) (map[string]string, error) {
	m := make(map[string]string)
	keepGoing := true
	prPage := 100
	endCursor := ""
	for keepGoing {
		page, err := queryForUsersPage(authToken, prPage, endCursor)
		if err != nil {
			return nil, err
		}
		maps.Copy(m, page.AsMap())
		keepGoing = page.Data.Organization.SamlIdentityProvider.ExternalIdentities.PageInfo.HasNextPage
		endCursor = page.Data.Organization.SamlIdentityProvider.ExternalIdentities.PageInfo.EndCursor
	}

	return m, nil
}

func queryForUsersPage(authToken string, prPage int, endCursor string) (*SamlUsersResponse, error) {
	query := strings.Replace(samlUsersQuery, "$FIRST", strconv.Itoa(prPage), 1)
	query = strings.Replace(query, "$AFTER", endCursor, 1)
	query = strings.Replace(query, "\n", " ", -1)
	reqBody := []byte(`{ "query": "` + query + ` }"`)
	users, err := httpsupport.MakeGqlRequest[SamlUsersResponse]("https://api.github.com/graphql", authToken, reqBody)
	if err != nil {
		return new(SamlUsersResponse), err
	}
	return users, nil
}

var samlUsersQuery = `
query { 
  organization(login: "navikt") { 
    samlIdentityProvider { 
      externalIdentities(first: $FIRST, after: "$AFTER") { 
        pageInfo { 
          hasNextPage
          endCursor
        }
        edges { 
          node { 
            samlIdentity { 
              emails { 
                value 
              } 
            } 
            user { 
              login 
            } 
          } 
        } 
      } 
    } 
  } 
}
`

type SamlUsersResponse struct {
	Data struct {
		Organization struct {
			SamlIdentityProvider struct {
				ExternalIdentities struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Edges []struct {
						Node struct {
							SamlIdentity struct {
								Emails []struct {
									Value string `json:"value"`
								} `json:"emails"`
							} `json:"samlIdentity"`
							User struct {
								Login string `json:"login"`
							} `json:"user"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"externalIdentities"`
			} `json:"samlIdentityProvider"`
		} `json:"organization"`
	} `json:"data"`
}

func (resp *SamlUsersResponse) AsMap() map[string]string {
	m := make(map[string]string)
	for _, edge := range resp.Data.Organization.SamlIdentityProvider.ExternalIdentities.Edges {
		key := edge.Node.User.Login
		m[key] = edge.Node.SamlIdentity.Emails[0].Value
	}
	return m
}
