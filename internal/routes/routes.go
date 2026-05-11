package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/navikt/whodis/internal/github"
)

func GetRoot(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		user = "unknown"
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "Hello " + user.(string),
	})
}

func GetTest(c *gin.Context) {
	ghUser := c.Param("githubUser")
	email := github.EmailFor(ghUser)
	if email != "" {
		c.JSON(http.StatusOK, gin.H{ghUser: email})
	} else {
		c.Status(404)
	}
}

func GetLiveness(c *gin.Context) {
	c.Status(200)
}

func GetReadyness(c *gin.Context) {
	if github.UsersAreLoaded() {
		c.Status(200)
	} else {
		c.Status(412)
	}
}
