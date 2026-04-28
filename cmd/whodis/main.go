package main

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/navikt/whodis/internal/auth"
)

func getRoot(c *gin.Context) {
	raw, exists := c.Get("token")
	user := ""
	if exists {
		token := raw.(*jwt.Token)
		claims := token.Claims.(jwt.MapClaims)
		user = claims["sub"].(string)
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "Hello " + user,
	})
}

func getLiveness(c *gin.Context) {
	c.Status(200)
}

func main() {
	wellKnownURI := envOrBust("WELL_KNOWN_URI")
	router := gin.Default()
	err := router.SetTrustedProxies([]string{})
	if err != nil {
		panic(err)
	}

	unprotectedRoutes := router.Group("/internal")
	unprotectedRoutes.GET("/isalive", getLiveness)
	unprotectedRoutes.GET("/isready", getLiveness)

	protectedRoutes := router.Group("/")
	err = auth.Init(wellKnownURI)
	if err != nil {
		panic(err)
	}
	protectedRoutes.Use(auth.AuthnInterceptor())
	protectedRoutes.GET("/", getRoot)

	err = router.Run(":8080")
	if err != nil {
		panic(err)
	}
}

func envOrBust(key string) string {
	value := os.Getenv(key)
	if value == "" {
		panic("unable not find environment variable " + key)
	}
	return value
}
