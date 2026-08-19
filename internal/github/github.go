package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/navikt/whodis/internal/httpsupport"
	"go.opentelemetry.io/otel/trace"
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

func (c *Client) AdminTeamsFor(repoName string, ctx context.Context) ([]string, error) {
	allTeams, err := c.allTeamsForRepo(repoName, ctx)
	if err != nil {
		return nil, err
	}
	adminSlugsMinusOrgAdmins := c.filterUnwanted(allTeams.AdminOnlySlugs(), c.teamsToSkip)
	return adminSlugsMinusOrgAdmins, nil
}

func (c *Client) AllTeamsFor(repoName string, ctx context.Context) ([]string, error) {
	allTeams, err := c.allTeamsForRepo(repoName, ctx)
	if err != nil {
		return nil, err
	}
	allSlugsMinusOrgAdmins := c.filterUnwanted(allTeams.AllSlugs(), c.teamsToSkip)
	return allSlugsMinusOrgAdmins, nil
}

func (c *Client) allTeamsForRepo(repoName string, ctx context.Context) (teamResponse, error) {
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
	return allRepoTeams, nil
}

func (c *Client) ReposForTeam(teamSlug string, ctx context.Context) ([]string, error) {
	span := trace.SpanFromContext(ctx)
	defer span.End()
	installationToken, err := c.retrieveAuthToken()
	if err != nil {
		return nil, err
	}
	var repos []string
	page := 1
	for {
		uri := fmt.Sprintf("%s/orgs/navikt/teams/%s/repos?per_page=100&page=%d", apiBaseURI, teamSlug, page)
		respBody, err := httpsupport.MakeAuthenticatedGetRequest(uri, installationToken)
		if err != nil {
			return nil, err
		}
		var batch []struct {
			FullName string `json:"full_name"`
		}
		if err := json.Unmarshal(respBody, &batch); err != nil {
			return nil, err
		}
		for _, r := range batch {
			repos = append(repos, r.FullName)
		}
		if len(batch) < 100 {
			break
		}
		page++
	}
	return repos, nil
}

type NaisDeployment struct {
	Cluster      string
	Namespace    string
	WorkflowFile string
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
