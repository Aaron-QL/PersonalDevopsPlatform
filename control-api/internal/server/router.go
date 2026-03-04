package server

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func NewRouter(mongoClient *mongo.Client) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", handleHealthz)
	r.GET("/readyz", handleReadyz(mongoClient))

	v1 := r.Group("/api/v1")
	_ = v1 // routes will be registered here in later phases

	return r
}

func handleHealthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func handleReadyz(mongoClient *mongo.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()

		if err := mongoClient.Ping(ctx, nil); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "mongodb unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}
