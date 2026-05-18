package github

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/navikt/whodis/internal/httpsupport"
)

var apiBaseURI = "https://api.github.com"

type Client struct {
	pkPEM     string
	clientId  string
	installId string
	orgUsers  map[string]string
	orgAdmins []string
}

func New(appPrivateKeyPem, appClientId, appInstallationId string) *Client {
	c := &Client{
		pkPEM:     appPrivateKeyPem,
		clientId:  appClientId,
		installId: appInstallationId,
		orgUsers:  make(map[string]string),
	}
	go c.syncSemiStaticDataPeriodically()
	return c
}

func (c *Client) Ping() error {
	if _, err := httpsupport.MakeGetRequest(apiBaseURI); err != nil {
		return err
	}
	return nil
}

func (c *Client) OwnersFor(repoName string) ([]string, error) {
	allRepoAdmins, err := httpsupport.MakeTypedRequest[[]string](apiBaseURI + "/repos/navikt/" + repoName + "/collaborators?permission=admin")
	if err != nil {
		return nil, err
	}
	return c.filterOutOrgAdmins(*allRepoAdmins), nil
}

func (c *Client) SemiStaticDataIsLoaded() bool {
	return len(c.orgUsers) > 0 && len(c.orgAdmins) > 0
}

func (c *Client) syncSemiStaticDataPeriodically() {
	c.loadOrgUsers()
	c.loadOrgAdmins()
	for range time.Tick(time.Hour * 12) {
		c.loadOrgUsers()
		c.loadOrgAdmins()
	}
}

func (c *Client) loadOrgUsers() {
	installationToken, err := c.retrieveAuthToken()
	if err != nil {
		fmt.Printf("error loading all users: %v\n", err)
		return
	}
	m := make(map[string]string)
	keepGoing := true
	prPage := 100
	endCursor := ""
	for keepGoing {
		page, err := c.queryForUsersPage(installationToken, prPage, endCursor)
		if err != nil {
			fmt.Printf("error loading all users: %v\n", err)
			return
		}
		maps.Copy(m, page.AsMap())
		keepGoing = page.Data.Organization.SamlIdentityProvider.ExternalIdentities.PageInfo.HasNextPage
		endCursor = page.Data.Organization.SamlIdentityProvider.ExternalIdentities.PageInfo.EndCursor
	}

	fmt.Printf("Loaded %d users from GitHub\n", len(m))
	c.orgUsers = m
}

func (c *Client) loadOrgAdmins() {
	httpResponse, err := httpsupport.MakeGetRequest(apiBaseURI + "/orgs/navikt/members?role=admin")
	if err != nil {
		fmt.Printf("Error loading org admins: %v\n", err)
	}
	var admins []usersResponse
	if err := json.Unmarshal(httpResponse, &admins); err != nil {
		fmt.Printf("Error loading org admins: %v\n", err)
	}
	var usernames []string
	for _, user := range admins {
		usernames = append(usernames, user.Login)
	}
	fmt.Printf("Loaded %d org admins", len(usernames))
	c.orgAdmins = usernames
}

func (c *Client) queryForUsersPage(authToken string, prPage int, endCursor string) (*samlUsersResponse, error) {
	fmt.Printf("Querying for users page: %s\n", endCursor)
	query := strings.Replace(samlUsersQuery, "$FIRST", strconv.Itoa(prPage), 1)
	query = strings.Replace(query, "$AFTER", endCursor, 1)
	query = strings.Replace(query, "\n", " ", -1)
	reqBody := []byte(`{ "query": " ` + query + ` " }`)
	page, err := httpsupport.MakeGqlRequest[samlUsersResponse](apiBaseURI+"/graphql", authToken, reqBody)
	if err != nil {
		return new(samlUsersResponse), err
	}
	return page, nil
}

func (c *Client) retrieveAuthToken() (string, error) {
	exchangeToken, err := c.createExchangeToken()
	if err != nil {
		return "", err
	}
	responseBody, err := httpsupport.MakePostRequest(apiBaseURI+"/app/installations/"+c.installId+"/access_tokens", exchangeToken, nil)
	if err != nil {
		return "", err
	}
	var tokenExchangeResult tokenExchangeResult
	if err := json.Unmarshal(responseBody, &tokenExchangeResult); err != nil {
		return "", err
	}
	return tokenExchangeResult.Token, nil
}

func (c *Client) createExchangeToken() (string, error) {
	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(c.pkPEM))
	if err != nil {
		return "", err
	}
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iat": now.Unix(),
		"exp": now.Add(time.Second * 30).Unix(),
		"iss": c.clientId,
	})
	serialized, err := token.SignedString(privateKey)
	if err != nil {
		return "", err
	}
	return serialized, nil
}

func (c *Client) filterOutOrgAdmins(usernames []string) []string {
	var filtered []string
	for _, user := range usernames {
		if !slices.Contains(c.orgAdmins, user) {
			filtered = append(filtered, user)
		}
	}
	return filtered
}

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
	Token string `json:"token"`
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
	fmt.Printf("Got %d users from GitHub with %d errors\n", len(m), errorCont)
	return m
}

type usersResponse struct {
	Login string `json:"login"`
}
