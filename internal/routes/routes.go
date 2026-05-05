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
	users, err := github.AllUsers("yolobogus")
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"users": users,
	})
}

func GetLiveness(c *gin.Context) {
	c.Status(200)
}
