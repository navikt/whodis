package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/navikt/whodis/internal/httpsupport"
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
		authHeader := ctx.GetHeader("Authorization")
		if authHeader == "" {
			fmt.Println("----------")
			_ = ctx.AbortWithError(http.StatusUnauthorized, errors.New("no authorization header found"))
			return
		}
		token, err := authenticateRequest(authHeader)
		if err != nil {
			_ = ctx.AbortWithError(http.StatusForbidden, err)
			return
		}

		ctx.Set("user", userFrom(token))
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
		jwt.WithValidMethods([]string{"RS256"}))
	if err != nil {
		return nil, err
	}
	aud, err := parsed.Claims.GetAudience()
	if err != nil {
		return nil, err
	}
	fmt.Printf("Audience is %v", aud)
	return parsed, nil
}

type WellKnownInfo struct {
	JwksUri string `json:"jwks_uri"`
}

func jwksURI(wellKnownURI string) (string, error) {
	wk, err := httpsupport.MakeGetRequest[WellKnownInfo](wellKnownURI)
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

func userFrom(token *jwt.Token) string {
	if token == nil {
		return "unknown"
	}
	claims := token.Claims.(jwt.MapClaims)
	subject := claims["sub"].(string)
	return subject
}
