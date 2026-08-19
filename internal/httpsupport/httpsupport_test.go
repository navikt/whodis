package httpsupport

import "testing"

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
