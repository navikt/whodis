package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/navikt/whodis/internal/httpsupport"
)

var pubKeyProvider keyfunc.Keyfunc

func Init(wellKnownURI string) error {
	jwksURI, err := jwksURI(wellKnownURI)
	if err != nil {
		return err
	}
	fmt.Println("Loaded public key from " + jwksURI)
	kf, err := keyfunc.NewDefault([]string{jwksURI})
	if err != nil {
		return err
	}
	pubKeyProvider = kf
	return nil
}

func JWTMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawHeader := r.Header.Get("Authorization")
		if rawHeader == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		tokenString := extractToken(rawHeader)
		parsed, err := jwt.Parse(
			tokenString,
			pubKeyProvider.Keyfunc,
			jwt.WithValidMethods([]string{"RS256"}))

		if err != nil || !parsed.Valid {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type wellKnownInfo struct {
	JwksUri string `json:"jwks_uri"`
}

func jwksURI(wellKnownURI string) (string, error) {
	responseBody, err := httpsupport.MakeGetRequest(wellKnownURI)
	if err != nil {
		return "", err
	}
	var info wellKnownInfo
	if err := json.Unmarshal(responseBody, &info); err != nil {
		return "", err
	}
	return info.JwksUri, nil
}

func extractToken(authHeaderValue string) string {
	if !strings.Contains(authHeaderValue, "Bearer ") {
		return ""
	}
	idxOfSplit := len("Bearer ")
	token := authHeaderValue[idxOfSplit:]
	return strings.TrimSpace(token)
}
