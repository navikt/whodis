package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

var pubKeyProvider keyfunc.Keyfunc

func Init(wellKnownURI string) error {
	jwksURI, err := jwksURI(wellKnownURI)
	if err != nil {
		return err
	}
	kf, err := keyfunc.NewDefault([]string{jwksURI})
	if err != nil {
		return err
	}
	pubKeyProvider = kf
	return nil
}

func AuthnInterceptor() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token, err := authenticateRequest(ctx.GetHeader("Authorization"))
		if err != nil {
			_ = ctx.AbortWithError(http.StatusForbidden, err)
		}
		ctx.Set("token", token)
		ctx.Next()
	}
}

func authenticateRequest(rawHeader string) (*jwt.Token, error) {
	token := extractToken(rawHeader)
	if token == "" {
		return nil, errors.New("token not found in Authorization header")
	}
	parsed, err := jwt.Parse(
		token,
		pubKeyProvider.Keyfunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithAudience("yolo"))
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

type WellKnownInfo struct {
	JwksUri string `json:"jwks_uri"`
}

func jwksURI(wellKnownURI string) (string, error) {
	resp, err := http.Get(wellKnownURI)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	wk := WellKnownInfo{}
	err = json.Unmarshal(body, &wk)
	if err != nil {
		return "", err
	}
	return wk.JwksUri, nil
}

func extractToken(authHeaderValue string) string {
	if !strings.Contains(authHeaderValue, "Bearer ") {
		return ""
	}
	idxOfSplit := len("Bearer ")
	token := authHeaderValue[idxOfSplit:]
	return strings.TrimSpace(token)
}
