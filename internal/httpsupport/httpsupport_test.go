package httpsupport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestErrorsInGqlResponseIsDetected(t *testing.T) {
	rawResponse := `{"data":{"organization":{"samlIdentityProvider":null}},"errors":[{"type":"FORBIDDEN","path":["organization","samlIdentityProvider"],"extensions":{"saml_failure":false},"locations":[{"line":1,"column":48}],"message":"Resource not accessible by integration"}]}`
	if err := gqlError([]byte(rawResponse)); err == nil {
		t.Errorf("gqlError(%v) = nil, expected error", rawResponse)
	}
}

func TestErrorsInGqlResponseEmptyArray(t *testing.T) {
	rawResponse := `{"data":{"organization":{"samlIdentityProvider":null}},"errors":[]}`
	if err := gqlError([]byte(rawResponse)); err != nil {
		t.Errorf("gqlError(%v) = %v, expected nil", rawResponse, err)
	}
}

func TestErrorsInGqlResponseNoArrayPresent(t *testing.T) {
	rawResponse := `{"data":{"organization":{"samlIdentityProvider":null}}}`
	if err := gqlError([]byte(rawResponse)); err != nil {
		t.Errorf("gqlError(%v) = %v, expected nil", rawResponse, err)
	}
}

func TestRequestsStopWhenContextIsDone(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := MakeAuthenticatedGetRequest(ctx, server.URL, "token"); err == nil {
		t.Fatal("expected the request to fail when its context expires")
	}
	if _, err := MakeUnauthenticatedGetRequest(ctx, server.URL); err == nil {
		t.Fatal("expected the request to fail when its context expires")
	}
	if _, err := MakePostRequest(ctx, server.URL, "token", nil); err == nil {
		t.Fatal("expected the request to fail when its context expires")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("requests ignored the context deadline, took %s", elapsed)
	}
}
