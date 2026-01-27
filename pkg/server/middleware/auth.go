package middleware

import (
	"food-agent/pkg/server/encoding"
	"food-agent/pkg/server/errutil"
	"food-agent/pkg/token"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// authManager 是包内私有的认证管理器
type authManager struct {
	tokenManager token.Manager
}

// 包内私有的全局变量
var manager *authManager

// Init 初始化认证管理器
func Init(tokenManager token.Manager) {
	manager = &authManager{
		tokenManager: tokenManager,
	}
}

// AuthMiddleware 认证中间件
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if manager == nil {
			encoding.HandleError(c, errutil.ErrUnauthorized)
			zap.L().Warn("AuthMiddleware tokenManager is nil")
			return
		}
		// 从context中获取token
		tokenString, err := manager.tokenManager.GetTokenFromCtx(c.Request.Context())
		if err != nil {
			encoding.HandleError(c, errutil.ErrUnauthorized)
			zap.L().Warn("Get Token Error")
			return
		}
		// 验证token
		info, err := manager.tokenManager.Verify(tokenString)
		if err != nil {
			encoding.HandleError(c, errutil.ErrUnauthorized)
			zap.L().Warn("Verify Token Error")
			return
		}
		// 将用户信息存储到context中
		c.Set("token_info", info)
		c.Set("user_id", info.UID)
		c.Set("username", info.Username)
		// 将token信息添加到context中
		newCtx := token.WithPayload(c.Request.Context(), info)
		c.Request = c.Request.WithContext(newCtx)
		c.Next()
	}
}
