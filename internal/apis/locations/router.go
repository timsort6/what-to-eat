package locations

import "github.com/gin-gonic/gin"

type Deps struct {
	Service      *Service
	Authenticate gin.HandlerFunc
}

func RegisterRoutes(router gin.IRouter, deps Deps) error {
	if deps.Service == nil || deps.Authenticate == nil {
		return errDependency
	}
	h := NewHandler(deps.Service)
	router.Group("", deps.Authenticate).GET("/locations/search", h.Search)
	return nil
}
