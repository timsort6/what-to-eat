package helper

import "github.com/gin-gonic/gin"

// RegisterRoutes 注册不依赖配置项的基础探活接口。
//
// 约定：传入的 router 已经是 /api/v1 分组（例如 r.Group("/api/v1")）。
func RegisterRoutes(router gin.IRouter) {
	router.GET("/ping", Ping)
	router.GET("/healthz", Healthz)
	router.GET("/readyz", Readyz)
}

