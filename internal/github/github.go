package github

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/navikt/whodis/internal/httpsupport"
	"gopkg.in/yaml.v3"
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
		orgAdmins: make([]string, 0),
	}
	go c.syncSemiStaticDataPeriodically()
	return c
}

func (c *Client) Ping() error {
	if _, err := httpsupport.MakeUnauthenticatedGetRequest(apiBaseURI); err != nil {
		return err
	}
	return nil
}

func (c *Client) EmailFor(username string) string {
	return c.orgUsers[username]
}

func (c *Client) AdminsFor(repoName string) ([]string, error) {
	uri := apiBaseURI + "/repos/navikt/" + repoName + "/collaborators?permission=admin"
	installationToken, err := c.retrieveAuthToken()
	if err != nil {
		return nil, err
	}
	respBody, err := httpsupport.MakeAuthenticatedGetRequest(uri, installationToken)
	if err != nil {
		return nil, err
	}
	var allRepoAdmins []usersResponse
	if err := json.Unmarshal(respBody, &allRepoAdmins); err != nil {
		return nil, err
	}
	var repoAdminLogins []string
	for _, repoAdmin := range allRepoAdmins {
		repoAdminLogins = append(repoAdminLogins, repoAdmin.Login)
	}
	return c.filterOutOrgAdmins(repoAdminLogins), nil
}

func (c *Client) WhereIsItDeployed(repoName string) ([]NaisDeployment, error) {
	installationToken, err := c.retrieveAuthToken()
	if err != nil {
		return nil, err
	}
	commitHash, err := c.latestCommit(repoName, installationToken)
	if err != nil {
		return nil, err
	}
	allFiles, err := c.filesIn(repoName, commitHash, installationToken)
	if err != nil {
		return nil, err
	}
	deploymentTasks, err := c.extractNaisDeployTasks(allFiles)
	if err != nil {
		return nil, err
	}
	var deployments []NaisDeployment
	for cluster, naisYamlPath := range deploymentTasks {
		naisYaml, err := c.naisYamlContents(repoName, naisYamlPath, installationToken)
		if err != nil {
			return nil, err
		}
		deployments = append(deployments, NaisDeployment{
			Cluster:   cluster,
			Namespace: naisYaml.Metadata.Namespace,
		})
	}
	return deployments, nil
}

func (c *Client) extractNaisDeployTasks(allFilesInRepo []string) (map[string]string, error) {
	var workflowFiles []string
	for _, file := range allFilesInRepo {
		if strings.HasPrefix(file, "./github/workflows") {
			workflowFiles = append(workflowFiles, file)
		}
	}
	var deployments map[string]string
	for _, wfFile := range allFilesInRepo {
		var workflow workflowFile
		if err := json.Unmarshal([]byte(wfFile), &workflow); err != nil {
			return nil, err
		}
		for _, job := range workflow.Jobs {
			for _, step := range job.Steps {
				if strings.HasPrefix(step.Uses, "nais/deploy/actions/deploy") {
					deployments[step.Env["CLUSTER"]] = step.Env["RESOURCE"]
				}
			}
		}
	}
	return deployments, nil
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
		slog.Error("error loading all users", slog.Any("error", err))
		return
	}
	m := make(map[string]string)
	keepGoing := true
	prPage := 100
	endCursor := ""
	for keepGoing {
		page, err := c.queryForUsersPage(installationToken, prPage, endCursor)
		if err != nil {
			slog.Error("error loading all users", slog.Any("error", err))
			return
		}
		maps.Copy(m, page.AsMap())
		keepGoing = page.Data.Organization.SamlIdentityProvider.ExternalIdentities.PageInfo.HasNextPage
		endCursor = page.Data.Organization.SamlIdentityProvider.ExternalIdentities.PageInfo.EndCursor
	}

	c.orgUsers = m
	slog.Info("Loaded users from GitHub", slog.Int("ghUsers", len(c.orgUsers)))
}

func (c *Client) loadOrgAdmins() {
	installationToken, err := c.retrieveAuthToken()
	httpResponse, err := httpsupport.MakeAuthenticatedGetRequest(apiBaseURI+"/orgs/navikt/members?role=admin", installationToken)
	if err != nil {
		slog.Error("Error loading org admins", slog.Any("error", err))
		return
	}
	var admins []usersResponse
	if err := json.Unmarshal(httpResponse, &admins); err != nil {
		slog.Error("Error loading org admins", slog.Any("error", err))
		return
	}
	var usernames []string
	for _, user := range admins {
		usernames = append(usernames, user.Login)
	}
	c.orgAdmins = usernames
	slog.Info("Loaded org admins\n", slog.Int("count", len(c.orgAdmins)))
}

func (c *Client) queryForUsersPage(authToken string, prPage int, endCursor string) (*samlUsersResponse, error) {
	query := strings.Replace(samlUsersQuery, "$FIRST", strconv.Itoa(prPage), 1)
	query = strings.Replace(query, "$AFTER", endCursor, 1)
	page, err := httpsupport.MakeGqlQuery[samlUsersResponse](apiBaseURI+"/graphql", authToken, query)
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

func (c *Client) filterOutOrgAdmins(repoAdmins []string) []string {
	var filtered []string
	for _, repoAdmin := range repoAdmins {
		if !slices.Contains(c.orgAdmins, repoAdmin) {
			filtered = append(filtered, repoAdmin)
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
	slog.Info("Loaded users from GitHub", slog.Int("count", len(m)), slog.Int("errors", errorCont))
	return m
}

type usersResponse struct {
	Login string `json:"login"`
}

type singleCommit struct {
	SHA string `json:"sha"`
}

type treeResponse struct {
	Leafs []treeLeaf `json:"tree"`
}

type treeLeaf struct {
	Path string `json:"path"`
	Size int    `json:"size"`
}

type workflowFile struct {
	Jobs map[string]struct {
		Steps []struct {
			Uses string            `json:"uses"`
			Env  map[string]string `json:"env"`
		}
	}
}

type fileReadResponse struct {
	ContentAsBase64 string `json:"content"`
}

type naisYaml struct {
	Metadata struct {
		Namespace string `yaml:"namespace"`
	}
}

type NaisDeployment struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
}

func (c *Client) latestCommit(repo string, authToken string) (string, error) {
	uri := apiBaseURI + "/repos/navikt/" + repo + "/commits"
	respBody, err := httpsupport.MakeAuthenticatedGetRequest(uri, authToken)
	if err != nil {
		return "", err
	}
	var commitResponse []singleCommit
	if err := json.Unmarshal(respBody, &commitResponse); err != nil {
		return "", err
	}
	return commitResponse[0].SHA, nil
}

func (c *Client) filesIn(repo string, commitSHA string, authToken string) ([]string, error) {
	uri := apiBaseURI + "/repos/navikt/" + repo + "/git/trees/" + commitSHA + "?recursive=true"
	respBody, err := httpsupport.MakeAuthenticatedGetRequest(uri, authToken)
	if err != nil {
		return nil, err
	}
	var fileTree treeResponse
	if err := json.Unmarshal(respBody, &fileTree); err != nil {
		return nil, err
	}
	var files []string
	for _, leaf := range fileTree.Leafs {
		files = append(files, leaf.Path)
	}
	return files, nil
}

func (c *Client) naisYamlContents(repo string, filePath string, authToken string) (*naisYaml, error) {
	fileContents, err := c.getFileContents(repo, filePath, authToken)
	if err != nil {
		return nil, err
	}
	var naisYaml naisYaml
	if err := yaml.Unmarshal(fileContents, &naisYaml); err != nil {
		return nil, err
	}
	return &naisYaml, nil
}

func (c *Client) getFileContents(repo string, filePath string, authToken string) ([]byte, error) {
	uri := apiBaseURI + "/repos/navikt/" + repo + "/contents/" + filePath
	respBody, err := httpsupport.MakeAuthenticatedGetRequest(uri, authToken)
	if err != nil {
		return nil, err
	}
	var resp fileReadResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, err
	}
	return c.extractTextFrom(resp)
}

func (c *Client) extractTextFrom(resp fileReadResponse) ([]byte, error) {
	b64Content := resp.ContentAsBase64
	decoded, err := base64.URLEncoding.DecodeString(b64Content)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}
