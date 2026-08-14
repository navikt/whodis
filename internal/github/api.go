package github

import "log/slog"

var samlUsersQuery = `query {
  organization(login: \"navikt\") {
    samlIdentityProvider {
      externalIdentities(first: $FIRST, after: \"$AFTER\") {
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

type samlUsersResponse struct {
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

type tokenExchangeResult struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

func (resp *samlUsersResponse) AsMap() map[string]string {
	m := make(map[string]string)
	errorCont := 0
	for _, edge := range resp.Data.Organization.SamlIdentityProvider.ExternalIdentities.Edges {
		if edge.Node.User.Login == "" || len(edge.Node.SamlIdentity.Emails) == 0 {
			errorCont += 1
			continue
		}
		key := edge.Node.User.Login
		m[key] = edge.Node.SamlIdentity.Emails[0].Value
	}
	return m
}

type usersResponse struct {
	Login string `json:"login"`
}

type teamResponse []struct {
	Slug       string `json:"slug"`
	Permission string `json:"permission"`
}

func (tr *teamResponse) AllSlugs() []string {
	var slugs []string
	for _, team := range *tr {
		slugs = append(slugs, team.Slug)
	}
	return slugs
}

func (tr *teamResponse) AdminOnlySlugs() []string {
	var slugs []string
	for _, team := range *tr {
		slog.Info("filtering slugs", slog.String("slug", team.Slug), slog.String("permission", team.Permission))
		if team.Permission == "admin" {
			slugs = append(slugs, team.Slug)
		}
	}
	return slugs
}
