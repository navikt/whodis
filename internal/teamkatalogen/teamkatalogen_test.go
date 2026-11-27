package teamkatalogen

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const membershipBody = `{"teams":[{"id":"1","name":"Team","slackChannel":"#team","naisTeams":["a","b"],"status":"ACTIVE"}]}`

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stubTeamkatalogen(t *testing.T, status int, body string) *atomic.Int32 {
	t.Helper()
	var requests atomic.Int32
	previous := http.DefaultTransport
	http.DefaultTransport = roundTripper(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if r.URL.Scheme+"://"+r.URL.Host != apiBaseURI || r.URL.Path != "/member/membership/byUserEmail" {
			t.Errorf("unexpected request %s", r.URL)
		}
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	return &requests
}

func TestDetailsForUserIsCachedPerEmailAndCopySafe(t *testing.T) {
	requests := stubTeamkatalogen(t, http.StatusOK, membershipBody)
	client := New()

	first, err := client.DetailsForUser("ola@nav.no", context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first.Teams[0].SlackChannel = "#mutated"
	first.Teams[0].NaisTeams[0] = "mutated"
	second, err := client.DetailsForUser("ola@nav.no", context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if second.Teams[0].SlackChannel != "#team" || !reflect.DeepEqual(second.Teams[0].NaisTeams, []string{"a", "b"}) {
		t.Errorf("cached details were corrupted by a caller: %+v", second)
	}
	if requests.Load() != 1 {
		t.Errorf("expected 1 upstream request, got %d", requests.Load())
	}

	if _, err := client.DetailsForUser("kari@nav.no", context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Errorf("a different email must not share the entry, got %d requests", requests.Load())
	}
}

func TestConcurrentLookupsForSameEmailShareOneRequest(t *testing.T) {
	requests := stubTeamkatalogen(t, http.StatusOK, membershipBody)
	client := New()

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.DetailsForUser("ola@nav.no", context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	if requests.Load() != 1 {
		t.Errorf("expected 1 upstream request, got %d", requests.Load())
	}
}

func TestFailuresAreNotCached(t *testing.T) {
	requests := stubTeamkatalogen(t, http.StatusInternalServerError, ``)
	client := New()

	for range 2 {
		if _, err := client.DetailsForUser("ola@nav.no", context.Background()); err == nil {
			t.Fatal("expected an error")
		}
	}
	if requests.Load() != 2 {
		t.Errorf("expected 2 upstream requests, got %d", requests.Load())
	}
}

func TestClientWithoutCacheStillWorks(t *testing.T) {
	requests := stubTeamkatalogen(t, http.StatusOK, membershipBody)
	client := &Client{}

	for range 2 {
		if _, err := client.DetailsForUser("ola@nav.no", context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 2 {
		t.Errorf("expected every lookup to reach upstream, got %d", requests.Load())
	}
}
