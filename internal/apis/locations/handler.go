package locations

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"food-agent/pkg/server/encoding"
	"food-agent/pkg/server/errutil"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Search(c *gin.Context) {
	params, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		encoding.HandleError(c, errutil.ErrIllegalParameter)
		return
	}
	for key, values := range params {
		if (key != "query" && key != "city") || len(values) != 1 {
			encoding.HandleError(c, errutil.ErrIllegalParameter)
			return
		}
	}
	input, err := NewSearchInput(params.Get("query"), params.Get("city"))
	if err != nil {
		encoding.HandleError(c, errutil.ErrIllegalParameter)
		return
	}
	items, err := h.svc.Search(c.Request.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			encoding.HandleError(c, errutil.NewError(http.StatusGatewayTimeout, "location search timed out"))
		case errors.Is(err, context.Canceled):
			encoding.HandleError(c, errutil.NewError(http.StatusRequestTimeout, "location search canceled"))
		default:
			encoding.HandleError(c, errutil.NewError(http.StatusBadGateway, "location search unavailable"))
		}
		return
	}
	encoding.HandleSuccessList(c, int64(len(items)), items)
}
