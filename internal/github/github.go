package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/navikt/whodis/internal/httpsupport"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"
)

var apiBaseURI = "https://api.github.com"

type Client struct {
	pkPEM                   string
	clientId                string
	installId               string
	installationToken       string
	installationTokenExpiry time.Time
	orgUsers                map[string]string
	orgAdmins               []string
	teamsToSkip             []string
}

func New(appPrivateKeyPem, appClientId, appInstallationId string, teamsToSkip []string) *Client {
	c := &Client{
		pkPEM:       appPrivateKeyPem,
		clientId:    appClientId,
		installId:   appInstallationId,
		orgUsers:    make(map[string]string),
		orgAdmins:   make([]string, 0),
		teamsToSkip: teamsToSkip,
	}
	go c.syncSemiStaticDataPeriodically()
	return c
}

func (c *Client) Ping() error {
	installationToken, err := c.retrieveAuthToken()
	if err != nil {
		return err
	}
	if _, err := httpsupport.MakeAuthenticatedGetRequest(apiBaseURI, installationToken); err != nil {
		return err
	}
	return nil
}

func (c *Client) EmailFor(username string) string {
	return c.orgUsers[username]
}

func (c *Client) AdminsFor(repoName string, ctx context.Context) ([]string, error) {
	span := trace.SpanFromContext(ctx)
	defer span.End()
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
	return c.filterUnwanted(repoAdminLogins, c.orgAdmins), nil
}

func (c *Client) TeamsFor(repoName string, ctx context.Context) ([]string, error) {
	span := trace.SpanFromContext(ctx)
	defer span.End()
	uri := apiBaseURI + "/repos/navikt/" + repoName + "/teams"
	installationToken, err := c.retrieveAuthToken()
	if err != nil {
		return nil, err
	}
	respBody, err := httpsupport.MakeAuthenticatedGetRequest(uri, installationToken)
	if err != nil {
		return nil, err
	}
	var allRepoTeams teamResponse
	if err := json.Unmarshal(respBody, &allRepoTeams); err != nil {
		return nil, err
	}
	fmt.Printf("%v\n", allRepoTeams)
	fmt.Printf("%v\n", c.teamsToSkip)
	filtered := c.filterUnwanted(allRepoTeams.Names(), c.teamsToSkip)
	return filtered, nil
}

type NaisDeployment struct {
	Cluster      string
	Namespace    string
	WorkflowFile string
}

func (c *Client) WhereIsItDeployed(repoName string, ctx context.Context) ([]NaisDeployment, error) {
	span := trace.SpanFromContext(ctx)
	defer span.End()
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
	workflowFiles := c.filterWorkflowFiles(allFiles)
	workflowFileContents, err := c.getContentsIn(repoName, workflowFiles, installationToken)
	if err != nil {
		return nil, err
	}
	var naisDeployments []NaisDeployment
	for wfFilePath, wfFileContents := range workflowFileContents {
		var wf workflowFile
		if err := yaml.Unmarshal([]byte(wfFileContents), &wf); err != nil {
			return nil, fmt.Errorf("error unmarshalling workflow file: %v", err)
		}
		deployInfo := wf.deployInfo()
		if deployInfo == nil || isTemplated(deployInfo.resource) || isTemplated(deployInfo.cluster) {
			slog.Info("unable to figure out deploy info, probably because values are templated")
			continue
		}
		pathToFirstNaisYaml := strings.Split(deployInfo.resource, ",")[0]
		naisYamlContent, err := c.getContentsIn(repoName, []string{pathToFirstNaisYaml}, installationToken)
		if err != nil {
			return nil, err
		}
		var naisYaml naisYaml
		if err = yaml.Unmarshal([]byte(naisYamlContent[pathToFirstNaisYaml]), &naisYaml); err != nil {
			return nil, fmt.Errorf("error unmarshalling nais.yaml: %v", err)
		}
		naisDeployments = append(naisDeployments, NaisDeployment{
			Cluster:      deployInfo.cluster,
			WorkflowFile: wfFilePath,
			Namespace:    naisYaml.Metadata.Namespace,
		})
	}
	return naisDeployments, nil
}

func (c *Client) SemiStaticDataIsLoaded() bool {
	return len(c.orgUsers) > 0 && len(c.orgAdmins) > 0
}

func (c *Client) syncSemiStaticDataPeriodically() {
	mutex := sync.Mutex{}
	c.loadOrgUsers(&mutex)
	c.loadOrgAdmins(&mutex)
	for range time.Tick(time.Hour * 12) {
		c.loadOrgUsers(&mutex)
		c.loadOrgAdmins(&mutex)
	}
}

func (c *Client) loadOrgUsers(mutex *sync.Mutex) {
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

	mutex.Lock()
	c.orgUsers = m
	mutex.Unlock()
	slog.Info("Loaded users from GitHub", slog.Int("ghUsers", len(c.orgUsers)))
}

func (c *Client) loadOrgAdmins(mutex *sync.Mutex) {
	installationToken, err := c.retrieveAuthToken()
	if err != nil {
		slog.Error("Error retrieving auth token", slog.Any("error", err))
		return
	}
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
	mutex.Lock()
	c.orgAdmins = usernames
	mutex.Unlock()
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
	if !c.tokenShouldBeRefreshed(time.Now()) {
		return c.installationToken, nil
	}

	exchangeToken, err := c.createExchangeToken()
	if err != nil {
		return "", err
	}
	resp, err := httpsupport.MakePostRequest(apiBaseURI+"/app/installations/"+c.installId+"/access_tokens", exchangeToken, nil)
	if err != nil {
		return "", err
	}
	var tExRes tokenExchangeResult
	if err := json.Unmarshal(resp, &tExRes); err != nil {
		return "", err
	}
	tokenExpiry, err := time.Parse(time.RFC3339, tExRes.ExpiresAt)
	if err != nil {
		return "", err
	}
	c.installationToken = tExRes.Token
	c.installationTokenExpiry = tokenExpiry
	slog.Info("The new token expires at", slog.Time("time", tokenExpiry))
	return c.installationToken, nil
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

func (c *Client) filterUnwanted(orig []string, unwanted []string) []string {
	var filtered []string
	for _, o := range orig {
		if !slices.Contains(unwanted, o) {
			filtered = append(filtered, o)
		}
	}
	return filtered
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
		return nil, fmt.Errorf("file listing request: %v", err)
	}
	var fileTree treeResponse
	if err := json.Unmarshal(respBody, &fileTree); err != nil {
		return nil, fmt.Errorf("unmarshal file listing response: %v", err)
	}
	var files []string
	for _, leaf := range fileTree.Leafs {
		files = append(files, leaf.Path)
	}
	return files, nil
}

func (c *Client) getContentsIn(repo string, files []string, authToken string) (map[string]string, error) {
	slog.Info("Getting contents in files", slog.String("repo", repo), slog.Any("files", files))
	var fileContents = make(map[string]string, len(files))
	errs := make(chan error, 1)
	fileBaseURI := apiBaseURI + "/repos/navikt/" + repo + "/contents/"
	wg := sync.WaitGroup{}
	for _, filePath := range files {
		wg.Add(1)
		go func(fp string, errChan chan error) {
			defer wg.Done()
			uri := fileBaseURI + "/" + fp
			respBody, err := httpsupport.MakeAuthenticatedGetRequest(uri, authToken)
			if err != nil {
				errs <- err
				return
			}
			var frr fileReadResponse
			if err := json.Unmarshal(respBody, &frr); err != nil {
				errChan <- err
				return
			}
			fileTxt, err := c.extractTextFrom(frr)
			if err != nil {
				errs <- err
				return
			}
			if err := json.Unmarshal(respBody, &frr); err != nil {
				errChan <- err
				return
			}
			fileContents[fp] = fileTxt
		}(filePath, errs)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return fileContents, nil
}

func (c *Client) extractTextFrom(resp fileReadResponse) (string, error) {
	b64Content := strings.ReplaceAll(resp.ContentAsBase64, "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(b64Content)
	if err != nil {
		return "", fmt.Errorf("b64 decoding: %v", err)
	}
	return string(decoded), nil
}

func (c *Client) filterWorkflowFiles(allFiles []string) []string {
	var filtered []string
	for _, file := range allFiles {
		if strings.HasPrefix(file, ".github/workflows/") {
			filtered = append(filtered, file)
		}
	}
	return filtered
}

func (c *Client) tokenShouldBeRefreshed(now time.Time) bool {
	if c.installationToken == "" {
		slog.Debug("No GitHub token present. time to get a new one")
		return true
	}

	in10Mins := now.Add(10 * time.Minute)
	shouldRefresh := c.installationTokenExpiry.Before(in10Mins)
	slog.Debug("Should GitHub token be refreshed?", slog.Bool("refresh", shouldRefresh))
	return shouldRefresh
}

type deployInfo struct {
	resource string
	cluster  string
}

func (wff *workflowFile) deployInfo() *deployInfo {
	for _, job := range wff.Jobs {
		for _, step := range job.Steps {
			if strings.HasPrefix(step.Uses, "nais/deploy/actions/deploy") {
				return &deployInfo{
					cluster:  step.Env["CLUSTER"],
					resource: step.Env["RESOURCE"],
				}
			}
		}
	}
	return nil
}

func isTemplated(str string) bool {
	match, _ := regexp.MatchString("([{}]+)", str)
	return match
}

type workflowFile struct {
	Jobs map[string]struct {
		Steps []struct {
			Uses string            `json:"uses"`
			Env  map[string]string `json:"env"`
		}
	}
}

type naisYaml struct {
	Metadata struct {
		Namespace string `yaml:"namespace"`
	}
}
