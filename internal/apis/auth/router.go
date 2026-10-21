package auth

import (
	"food-agent/pkg/server/middleware"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes registers auth-related routes on the given router.
//
// Convention: router is already the /api/v1 group.
func RegisterRoutes(router gin.IRouter, deps Deps) error {
	h, err := NewHandler(deps)
	if err != nil {
		return err
	}

	router.POST("/auth/register", h.Register)
	router.POST("/auth/login", h.Login)

	// Protected endpoint.
	router.GET("/me", middleware.CheckToken(deps.Tokens), h.Me)
	return nil
}
