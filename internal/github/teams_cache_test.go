package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/navikt/whodis/internal/httpsupport"
)

const teamsBody = `[{"slug":"admin-team","permission":"admin"},{"slug":"reader-team","permission":"pull"},{"slug":"ignored-team","permission":"admin"}]`

func cachedClient() *Client {
	return &Client{
		installationToken:       "test-token",
		installationTokenExpiry: time.Now().Add(time.Hour),
		teamsToSkip:             []string{"ignored-team"},
		teamsCache:              newRepoTeamsCache(),
	}
}

// stubTeamsEndpoint replies with the given status and body and returns the number of upstream requests.
func stubTeamsEndpoint(t *testing.T, status int, body string) *atomic.Int32 {
	t.Helper()
	var requests atomic.Int32
	previousTransport := http.DefaultTransport
	http.DefaultTransport = repositoryTransport(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	return &requests
}

func mustRepository(t *testing.T, name string) RepositoryID {
	t.Helper()
	repository, err := NewRepositoryID("navikt", name)
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func TestAdminAndAllTeamsShareOneUpstreamRequest(t *testing.T) {
	requests := stubTeamsEndpoint(t, http.StatusOK, teamsBody)
	client := cachedClient()
	repository := mustRepository(t, "some-repo")

	admins, err := client.AdminTeamsFor(repository, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	all, err := client.AllTeamsFor(repository, context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(admins, []string{"admin-team"}) {
		t.Errorf("unexpected admin teams: %v", admins)
	}
	if !reflect.DeepEqual(all, []string{"admin-team", "reader-team"}) {
		t.Errorf("unexpected teams: %v", all)
	}
	if requests.Load() != 1 {
		t.Errorf("expected 1 upstream request, got %d", requests.Load())
	}
}

func TestRepositoryNameCasingSharesCacheEntry(t *testing.T) {
	requests := stubTeamsEndpoint(t, http.StatusOK, teamsBody)
	client := cachedClient()

	for _, name := range []string{"Some.Repo", "some.repo", "SOME.REPO"} {
		if _, err := client.AllTeamsFor(mustRepository(t, name), context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 {
		t.Errorf("expected 1 upstream request, got %d", requests.Load())
	}
}

func TestConcurrentLookupsForSameRepositoryShareOneUpstreamRequest(t *testing.T) {
	requests := stubTeamsEndpoint(t, http.StatusOK, teamsBody)
	client := cachedClient()
	repository := mustRepository(t, "some-repo")

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.AllTeamsFor(repository, context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	if requests.Load() != 1 {
		t.Errorf("expected 1 upstream request, got %d", requests.Load())
	}
}

func TestRepositoryNotFoundIsCachedAndKeepsHttpError(t *testing.T) {
	requests := stubTeamsEndpoint(t, http.StatusNotFound, `{"message":"Not Found"}`)
	client := cachedClient()
	repository := mustRepository(t, "missing")

	for range 2 {
		_, err := client.AllTeamsFor(repository, context.Background())
		var httpErr *httpsupport.HttpError
		if !errors.As(err, &httpErr) || httpErr.Code != http.StatusNotFound {
			t.Fatalf("expected an HTTP 404 error, got %v", err)
		}
	}
	if requests.Load() != 1 {
		t.Errorf("expected the 404 to be cached, got %d upstream requests", requests.Load())
	}
}

func TestUpstreamServerErrorIsNotCached(t *testing.T) {
	requests := stubTeamsEndpoint(t, http.StatusBadGateway, ``)
	client := cachedClient()
	repository := mustRepository(t, "some-repo")

	for range 2 {
		if _, err := client.AllTeamsFor(repository, context.Background()); err == nil {
			t.Fatal("expected an error")
		}
	}
	if requests.Load() != 2 {
		t.Errorf("5xx must not be cached, got %d upstream requests", requests.Load())
	}
}

func TestTeamsToSkipIsAppliedAfterTheCache(t *testing.T) {
	stubTeamsEndpoint(t, http.StatusOK, teamsBody)
	client := cachedClient()
	repository := mustRepository(t, "some-repo")

	if _, err := client.AllTeamsFor(repository, context.Background()); err != nil {
		t.Fatal(err)
	}
	client.teamsToSkip = []string{"reader-team"}
	all, err := client.AllTeamsFor(repository, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(all, []string{"admin-team", "ignored-team"}) {
		t.Errorf("cache must hold the unfiltered response, got %v", all)
	}
}

func TestReposForTeamIsCachedAndCopySafe(t *testing.T) {
	requests := stubTeamsEndpoint(t, http.StatusOK, `[{"full_name":"navikt/a"},{"full_name":"navikt/b"}]`)
	client := &Client{
		installationToken:       "test-token",
		installationTokenExpiry: time.Now().Add(time.Hour),
		teamReposCache:          newTeamReposCache(),
	}

	first, err := client.ReposForTeam("appsec", context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first[0] = "mutated"
	second, err := client.ReposForTeam("appsec", context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(second, []string{"navikt/a", "navikt/b"}) {
		t.Errorf("cached value was corrupted by a caller: %v", second)
	}
	if requests.Load() != 1 {
		t.Errorf("expected 1 upstream request, got %d", requests.Load())
	}
}

func TestReposForTeamNegativeResultsAreCachedButFailuresAreNot(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		wantErr      bool
		wantRequests int32
	}{
		{"team without repositories", http.StatusOK, `[]`, false, 1},
		{"unknown team", http.StatusNotFound, `{"message":"Not Found"}`, true, 1},
		{"upstream failure", http.StatusBadGateway, ``, true, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := stubTeamsEndpoint(t, tt.status, tt.body)
			client := &Client{
				installationToken:       "test-token",
				installationTokenExpiry: time.Now().Add(time.Hour),
				teamReposCache:          newTeamReposCache(),
			}
			for range 2 {
				_, err := client.ReposForTeam("appsec", context.Background())
				if (err != nil) != tt.wantErr {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if requests.Load() != tt.wantRequests {
				t.Errorf("expected %d upstream requests, got %d", tt.wantRequests, requests.Load())
			}
		})
	}
}

func TestTokenExchangeNotFoundIsNotCachedAsAbsence(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	requests := stubTeamsEndpoint(t, http.StatusNotFound, `{"message":"Not Found"}`)
	client := &Client{
		pkPEM:          pemKey,
		clientId:       "client-id",
		installId:      "install-id",
		teamsCache:     newRepoTeamsCache(),
		teamReposCache: newTeamReposCache(),
	}
	repository := mustRepository(t, "some-repo")

	for range 2 {
		if _, err := client.AllTeamsFor(repository, context.Background()); !errors.Is(err, errTokenExchange) {
			t.Fatalf("expected a token exchange error, got %v", err)
		}
		if _, err := client.ReposForTeam("some-team", context.Background()); !errors.Is(err, errTokenExchange) {
			t.Fatalf("expected a token exchange error, got %v", err)
		}
	}
	if requests.Load() != 4 {
		t.Errorf("token exchange failures must not be cached, expected 4 upstream requests, got %d", requests.Load())
	}
}
