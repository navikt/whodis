package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/navikt/whodis/internal/auth"
	"github.com/navikt/whodis/internal/github"
)

func main() {
	wellKnownURI := envOrBust("WELL_KNOWN_URI")
	err := auth.Init(wellKnownURI)
	if err != nil {
		panic(err)
	}

	ghApiToken := envOrBust("GITHUB_API_TOKEN")
	github.Init(ghApiToken)

	router := gin.New()
	skip := func(c *gin.Context) bool {
		return strings.HasPrefix(c.FullPath(), "/internal")
	}
	loggerConfig := gin.LoggerConfig{
		Skip: skip,
	}
	router.Use(ErrorHandler())
	router.Use(gin.LoggerWithConfig(loggerConfig))

	if err = router.SetTrustedProxies([]string{}); err != nil {
		panic(err)
	}

	unprotectedRoutes := router.Group("/internal")
	unprotectedRoutes.GET("/isalive", getLiveness)
	unprotectedRoutes.GET("/isready", getLiveness)

	protectedRoutes := router.Group("/")
	protectedRoutes.Use(auth.AuthnInterceptor())
	protectedRoutes.GET("/", getRoot)
	protectedRoutes.GET("/test", getTest)

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

func getRoot(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		user = "unknown"
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "Hello " + user.(string),
	})
}

func getTest(c *gin.Context) {
	users, err := github.AllUsers("yolobogus")
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"users": users,
	})
}

func getLiveness(c *gin.Context) {
	c.Status(200)
}

func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		fmt.Println(c.Errors)
		if len(c.Errors) > 0 {
			c.Status(500)
		}
	}
}
