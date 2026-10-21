package agentchat

import (
	"errors"

	"food-agent/pkg/server/middleware"
	"food-agent/pkg/token"

	"github.com/gin-gonic/gin"
)

type RouterDeps struct {
	Service *Service
	Tokens  token.Manager
}

// RegisterRoutes registers /agent/chat on the given router.
//
// Convention: router is already the /api/v1 group.
func RegisterRoutes(router gin.IRouter, deps RouterDeps) error {
	if deps.Service == nil {
		return errors.New("nil agentchat service")
	}
	if deps.Tokens == nil {
		return errors.New("nil token manager")
	}

	h := NewHandler(deps.Service)
	authed := router.Group("", middleware.CheckToken(deps.Tokens))
	authed.POST("/agent/chat", h.Chat)
	return nil
}
