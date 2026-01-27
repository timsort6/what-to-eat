package helper

import (
	"github.com/gin-gonic/gin"

	"food-agent/pkg/server/encoding"
)

func Ping(c *gin.Context) {
	encoding.HandleSuccess(c, PingResponse{Message: "pong"})
}

func Healthz(c *gin.Context) {
	encoding.HandleSuccess(c, HealthResponse{Status: "ok"})
}

func Readyz(c *gin.Context) {
	encoding.HandleSuccess(c, HealthResponse{Status: "ok"})
}
