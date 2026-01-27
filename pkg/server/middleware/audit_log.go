package middleware

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"time"
)

// AddAuditLog 轻量审计日志中间件（当前版本仅输出到 zap，不依赖数据库）。
func AddAuditLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		// 继续处理请求
		c.Next()
		zap.L().Info("AUDIT",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.String("client_ip", c.ClientIP()),
			zap.Duration("latency", time.Since(startTime)),
		)
	}
}
