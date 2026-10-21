package agentchat

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"food-agent/pkg/server/encoding"
	"food-agent/pkg/server/errutil"
	"food-agent/pkg/token"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Chat(c *gin.Context) {
	userID, ok := mustUserID(c)
	if !ok {
		return
	}

	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		encoding.HandleError(c, errutil.ErrJSONFormat)
		return
	}
	req.Location = strings.TrimSpace(req.Location)
	if req.AddressID == nil && req.Location == "" {
		encoding.HandleError(c, errutil.NewError(http.StatusBadRequest, "address_id or location is required"))
		return
	}

	prep, err := h.svc.Prepare(c.Request.Context(), userID, req)
	if err != nil {
		encoding.HandleError(c, mapPrepareErr(err))
		return
	}

	stream, err := h.svc.StreamAnswer(c.Request.Context(), prep)
	if err != nil {
		encoding.HandleError(c, errutil.ErrInternalServer)
		return
	}
	defer stream.Close()

	// SSE headers.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	// Disable proxy buffering (nginx).
	c.Header("X-Accel-Buffering", "no")

	var answer strings.Builder
	c.Stream(func(w io.Writer) bool {
		// Client gone.
		select {
		case <-c.Request.Context().Done():
			return false
		default:
		}

		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			done := DoneData{
				AnswerText:    answer.String(),
				Places:        prep.Places,
				PlacesCount:   len(prep.Places),
				Location:      prep.Location,
				UsedAddressID: prep.UsedAddressID,
			}
			c.SSEvent("done", done)
			return false
		}
		if err != nil {
			c.SSEvent("error", gin.H{"message": "The response could not be generated."})
			return false
		}

		if chunk != nil && chunk.Content != "" {
			answer.WriteString(chunk.Content)
			c.SSEvent("delta", gin.H{"text": chunk.Content})
		}
		return true
	})
}

func mustUserID(c *gin.Context) (uint, bool) {
	payload, err := token.PayloadFromCtx(c.Request.Context())
	if err != nil {
		encoding.HandleError(c, errutil.ErrUnauthorized)
		return 0, false
	}
	id64, err := strconv.ParseUint(strings.TrimSpace(payload.UID), 10, 64)
	if err != nil || id64 == 0 {
		encoding.HandleError(c, errutil.ErrUnauthorized)
		return 0, false
	}
	return uint(id64), true
}

func mapPrepareErr(err error) error {
	msg := strings.TrimSpace(err.Error())
	switch {
	case errors.Is(err, errInvalidInput):
		return errutil.NewError(http.StatusBadRequest, msg)
	case strings.Contains(strings.ToLower(msg), "permission denied"):
		return errutil.ErrPermissionDenied
	case strings.Contains(strings.ToLower(msg), "not found"):
		return errutil.ErrNotFound
	default:
		return errutil.ErrInternalServer
	}
}
