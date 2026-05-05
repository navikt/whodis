package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/navikt/whodis/internal/auth"
	"github.com/navikt/whodis/internal/github"
	"github.com/navikt/whodis/internal/routes"
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
	setupLogging(router)
	router.Use(errorHandler())

	if err = router.SetTrustedProxies([]string{}); err != nil {
		panic(err)
	}

	unprotectedRoutes := router.Group("/internal")
	unprotectedRoutes.GET("/isalive", routes.GetLiveness)
	unprotectedRoutes.GET("/isready", routes.GetLiveness)

	protectedRoutes := router.Group("/")
	protectedRoutes.Use(auth.AuthnInterceptor())
	protectedRoutes.GET("/", routes.GetRoot)
	protectedRoutes.GET("/test", routes.GetTest)

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

func errorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		fmt.Println(c.Errors)
		if len(c.Errors) > 0 {
			c.Status(500)
		}
	}
}

func setupLogging(router *gin.Engine) {
	skip := func(c *gin.Context) bool {
		return strings.HasPrefix(c.FullPath(), "/internal")
	}
	loggerConfig := gin.LoggerConfig{
		Skip: skip,
	}
	router.Use(gin.LoggerWithConfig(loggerConfig))
}
