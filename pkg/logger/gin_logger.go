package logger

import (
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GinLogger instances a Logger middleware that will write the logs to gin.DefaultWriter.
// By default gin.DefaultWriter = os.Stdout.
func GinLogger() gin.HandlerFunc {
	return LoggerWithWriter(gin.DefaultWriter)
}

// LoggerWithWriter instance a Logger middleware with the specified writter buffer.
// Example: os.Stdout, a file opened in write mode, a socket...
func LoggerWithWriter(out io.Writer, notlogged ...string) gin.HandlerFunc {
	var skip map[string]struct{}

	if length := len(notlogged); length > 0 {
		skip = make(map[string]struct{}, length)

		for _, path := range notlogged {
			skip[path] = struct{}{}
		}
	}

	return func(c *gin.Context) {
		// Start timer
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// Process request
		c.Next()

		// Log only when path is not being skipped
		if _, ok := skip[path]; !ok {
			// Stop timer
			latency := time.Since(start)
			clientIP := c.ClientIP()
			method := c.Request.Method
			statusCode := c.Writer.Status()
			comment := c.Errors.ByType(gin.ErrorTypePrivate).String()
			if raw != "" {
				path = path + "?" + raw
			}

			if comment == "" {
				zap.L().Info("GIN",
					zap.String("Path", path),
					zap.Int("StatusCode", statusCode),
					zap.String("Method", method),
					zap.String("ClientIP", clientIP),
					zap.Duration("Latency", latency),
				)
			} else {
				zap.L().Error("GIN",
					zap.String("Path", path),
					zap.Int("StatusCode", statusCode),
					zap.String("Method", method),
					zap.String("ClientIP", clientIP),
					zap.Duration("Latency", latency),
					zap.String("comment", comment),
				)
			}
		}
	}
}
