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
	"github.com/navikt/whodis/internal/cache"
	"github.com/navikt/whodis/internal/httpsupport"
	"go.opentelemetry.io/otel/trace"
)

var apiBaseURI = "https://api.github.com"

type Client struct {
	pkPEM                   string
	clientId                string
	installId               string
	tokenMu                 sync.Mutex
	installationToken       string
	installationTokenExpiry time.Time
	dataMu                  sync.RWMutex
	orgUsers                map[string]string
	orgAdmins               []string
	teamsToSkip             []string
	// teamsCache is optional; a nil cache means every lookup goes to GitHub.
	teamsCache *cache.Cache[teamResponse]
}

func New(appPrivateKeyPem, appClientId, appInstallationId string, teamsToSkip []string) *Client {
	c := &Client{
		pkPEM:       appPrivateKeyPem,
		clientId:    appClientId,
		installId:   appInstallationId,
		orgUsers:    make(map[string]string),
		orgAdmins:   make([]string, 0),
		teamsToSkip: teamsToSkip,
		teamsCache:  newRepoTeamsCache(),
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
	c.dataMu.RLock()
	defer c.dataMu.RUnlock()
	return c.orgUsers[username]
}

func (c *Client) AdminTeamsFor(repository RepositoryID, ctx context.Context) ([]string, error) {
	allTeams, err := c.allTeamsForRepo(repository, ctx)
	if err != nil {
		return nil, err
	}
	adminSlugsMinusOrgAdmins := c.filterUnwanted(allTeams.AdminOnlySlugs(), c.teamsToSkip)
	return adminSlugsMinusOrgAdmins, nil
}

func (c *Client) AllTeamsFor(repository RepositoryID, ctx context.Context) ([]string, error) {
	allTeams, err := c.allTeamsForRepo(repository, ctx)
	if err != nil {
		return nil, err
	}
	allSlugsMinusOrgAdmins := c.filterUnwanted(allTeams.AllSlugs(), c.teamsToSkip)
	return allSlugsMinusOrgAdmins, nil
}

func (c *Client) allTeamsForRepo(repository RepositoryID, ctx context.Context) (teamResponse, error) {
	span := trace.SpanFromContext(ctx)
	defer span.End()
	if c.teamsCache == nil {
		return c.fetchTeamsForRepo(repository)
	}
	// GitHub treats repository names case-insensitively, so one entry serves every spelling.
	key := strings.ToLower(repository.String())
	return c.teamsCache.GetOrLoad(ctx, key, func(context.Context) (teamResponse, error) {
		return c.fetchTeamsForRepo(repository)
	})
}

func (c *Client) fetchTeamsForRepo(repository RepositoryID) (teamResponse, error) {
	uri := apiBaseURI + "/repos/" + repository.String() + "/teams"
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
	c.dataMu.RLock()
	defer c.dataMu.RUnlock()
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

	c.dataMu.Lock()
	c.orgUsers = m
	c.dataMu.Unlock()
	slog.Info("Loaded users from GitHub", slog.Int("ghUsers", len(m)))
}

func (c *Client) loadOrgAdmins() {
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
	c.dataMu.Lock()
	c.orgAdmins = usernames
	c.dataMu.Unlock()
	slog.Info("Loaded org admins\n", slog.Int("count", len(usernames)))
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
	// Held across the refresh so concurrent callers wait for one new token instead of each fetching their own.
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
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
