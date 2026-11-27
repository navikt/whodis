package github

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentTokenRefreshFetchesOneToken(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testfiles", "private_key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	var tokenRequests atomic.Int32
	previousTransport := http.DefaultTransport
	http.DefaultTransport = repositoryTransport(func(r *http.Request) (*http.Response, error) {
		tokenRequests.Add(1)
		time.Sleep(50 * time.Millisecond)
		body := `{"token":"fresh-token","expires_at":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`
		return &http.Response{
			StatusCode: http.StatusCreated,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	client := &Client{pkPEM: string(data), clientId: "123", installId: "321"}

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := client.retrieveAuthToken(context.Background())
			if err != nil || token != "fresh-token" {
				t.Errorf("unexpected token %q, err %v", token, err)
			}
		}()
	}
	wg.Wait()

	if got := tokenRequests.Load(); got != 1 {
		t.Errorf("expected 1 token exchange, got %d", got)
	}
}

func TestSemiStaticDataReadsWhileReloading(t *testing.T) {
	client := &Client{orgUsers: map[string]string{"a": "a@nav.no"}, orgAdmins: []string{"admin"}}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 1000 {
			client.dataMu.Lock()
			client.orgUsers = map[string]string{"a": "a@nav.no"}
			client.orgAdmins = []string{"admin"}
			client.dataMu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		for range 1000 {
			if client.EmailFor("a") != "a@nav.no" || !client.SemiStaticDataIsLoaded() {
				t.Error("semi-static data unexpectedly missing")
				return
			}
		}
	}()
	wg.Wait()
}
