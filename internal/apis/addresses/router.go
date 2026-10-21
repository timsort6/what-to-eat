package addresses

import (
	"errors"

	"food-agent/pkg/server/middleware"
	"food-agent/pkg/token"

	"github.com/gin-gonic/gin"
)

type Deps struct {
	Service Service
	Tokens  token.Manager
}

// RegisterRoutes registers /addresses CRUD routes on the given router.
//
// Convention: router is already the /api/v1 group.
func RegisterRoutes(router gin.IRouter, deps Deps) error {
	if deps.Service == nil {
		return errors.New("nil addresses service")
	}
	if deps.Tokens == nil {
		return errors.New("nil token manager")
	}

	h := NewHandler(deps.Service)

	authed := router.Group("", middleware.CheckToken(deps.Tokens))
	authed.POST("/addresses", h.Create)
	authed.GET("/addresses", h.List)
	authed.PUT("/addresses/:id", h.Update)
	authed.DELETE("/addresses/:id", h.Delete)
	return nil
}
