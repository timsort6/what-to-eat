package agentchat

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"food-agent/internal/agent"
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
		logStreamError("start", err)
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
	var decision *agent.PublicDecision
	// 决策只在首个 delta 或 done 前发一次；取不到已验证决策则不能输出任何推荐内容。
	emitDecision := func() bool {
		if decision != nil {
			return true
		}
		snapshot, ok := stream.Decision()
		if !ok {
			return false
		}
		decision = &snapshot
		c.SSEvent("decision", snapshot)
		return true
	}
	failed := func(stage string, err error) bool {
		logStreamError(stage, err)
		c.SSEvent("error", gin.H{"message": "The response could not be generated."})
		return false
	}
	c.Stream(func(w io.Writer) bool {
		// Client gone.
		select {
		case <-c.Request.Context().Done():
			return false
		default:
		}

		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if c.Request.Context().Err() != nil {
				return false
			}
			if !emitDecision() {
				return failed("decision", errMissingDecision)
			}
			// 与决策同一快照的实际条件：请求品类为空且原话品类被采纳时含该品类。
			preferences, ok := stream.EffectivePreferences()
			if !ok {
				return failed("decision", errMissingDecision)
			}
			places := make([]Place, 0, len(stream.Places()))
			for _, place := range stream.Places() {
				places = append(places, projectPlace(place))
			}
			done := DoneData{
				AnswerText:    answer.String(),
				Places:        places,
				PlacesCount:   len(places),
				Location:      prep.Location,
				UsedAddressID: prep.UsedAddressID,
				Status:        decision.Status,
				Preferences:   preferences,
				Decision:      *decision,
			}
			c.SSEvent("done", done)
			return false
		}
		if err != nil {
			if c.Request.Context().Err() != nil {
				return false
			}
			return failed("receive", err)
		}

		if chunk != nil && chunk.Content != "" {
			if !emitDecision() {
				return failed("decision", errMissingDecision)
			}
			answer.WriteString(chunk.Content)
			c.SSEvent("delta", gin.H{"text": chunk.Content})
		}
		return true
	})
}

func projectPlace(source agent.Place) Place {
	place := Place{ID: source.ID, CandidateKey: source.CandidateKey, Name: source.Name, Location: source.Location,
		Address: source.Address, Distance: source.Distance, Type: source.Type, TypeCode: source.TypeCode, Pname: source.Pname,
		Cityname: source.Cityname, Adname: source.Adname, Citycode: source.Citycode, Adcode: source.Adcode, Tag: source.Tag,
		OpentimeToday: source.OpentimeToday, OpentimeWeek: source.OpentimeWeek, Evaluation: source.Evaluation}
	if source.Evaluation != nil && source.Evaluation.Price.State != agent.MatchUnknown {
		place.Cost, place.CostCurrency, place.CostBasis = source.Cost, source.CostCurrency, source.CostBasis
	}
	return place
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
