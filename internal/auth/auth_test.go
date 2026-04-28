package auth

import "testing"

func TestExtractTokenFromAuthHeader(t *testing.T) {
	rawHeaderValue := "Bearer 1234yolo"
	extractedToken := extractToken(rawHeaderValue)
	if extractedToken != "1234yolo" {
		t.Errorf("extracted token is '%s', should be '%s'", extractedToken, "1234yolo")
	}
}
