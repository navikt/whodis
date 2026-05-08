package httpsupport

import "testing"

func TestErrorsInGqlResponseIsDetected(t *testing.T) {
	rawResponse := `{"data":{"organization":{"samlIdentityProvider":null}},"errors":[{"type":"FORBIDDEN","path":["organization","samlIdentityProvider"],"extensions":{"saml_failure":false},"locations":[{"line":1,"column":48}],"message":"Resource not accessible by integration"}]}`
	isError := isError([]byte(rawResponse))
	if !isError {
		t.Errorf("isError(%v) = false, expected true", rawResponse)
	}
}

func TestErrorsInGqlResponseEmptyArray(t *testing.T) {
	rawResponse := `{"data":{"organization":{"samlIdentityProvider":null}},"errors":[]}`
	isError := isError([]byte(rawResponse))
	if isError {
		t.Errorf("isError(%v) = true, expected false", rawResponse)
	}
}

func TestErrorsInGqlResponseNoArrayPresent(t *testing.T) {
	rawResponse := `{"data":{"organization":{"samlIdentityProvider":null}}}`
	isError := isError([]byte(rawResponse))
	if isError {
		t.Errorf("isError(%v) = true, expected false", rawResponse)
	}
}
