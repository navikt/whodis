package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func getRoot(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "Hello World!",
	})
}

func getLiveness(c *gin.Context) {
	c.Status(200)
}

func main() {
	router := gin.Default()
	router.GET("/", getRoot)
	router.GET("/internal/isalive", getLiveness)
	router.GET("/internal/isready", getLiveness)
	err := router.Run(":8080")
	if err != nil {
		panic(err)
	}
}
