package auth

import (
	"errors"
	"net/http"
	"regexp"

	"food-agent/pkg/server/encoding"
	"food-agent/pkg/server/errutil"
	"food-agent/pkg/token"

	"github.com/gin-gonic/gin"
)

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

type Handler struct {
	svc *Service
}

type Deps struct {
	Users             UserDAO
	Tokens            token.Manager
	JWTExpiresSeconds int
}

func NewHandler(d Deps) (*Handler, error) {
	svc, err := NewService(d)
	if err != nil {
		return nil, err
	}
	return &Handler{
		svc: svc,
	}, nil
}

func (h *Handler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		encoding.HandleError(c, errutil.ErrJSONFormat)
		return
	}

	info, err := h.svc.Register(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		encoding.HandleError(c, mapServiceErr(err))
		return
	}
	encoding.HandleSuccess(c, info)
}

func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		encoding.HandleError(c, errutil.ErrJSONFormat)
		return
	}

	resp, err := h.svc.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		encoding.HandleError(c, mapServiceErr(err))
		return
	}
	encoding.HandleSuccess(c, resp)
}

func (h *Handler) Me(c *gin.Context) {
	payload, err := token.PayloadFromCtx(c.Request.Context())
	if err != nil {
		encoding.HandleError(c, errutil.ErrUnauthorized)
		return
	}
	info, err := h.svc.Me(c.Request.Context(), payload.UID)
	if err != nil {
		encoding.HandleError(c, mapServiceErr(err))
		return
	}
	encoding.HandleSuccess(c, info)
}

func validateDeps(d Deps) (UserDAO, token.Manager, int, error) {
	if d.Users == nil {
		return nil, nil, 0, errors.New("nil users store")
	}
	if d.Tokens == nil {
		return nil, nil, 0, errors.New("nil token manager")
	}
	if d.JWTExpiresSeconds <= 0 {
		return nil, nil, 0, errors.New("invalid jwt expires seconds")
	}
	return d.Users, d.Tokens, d.JWTExpiresSeconds, nil
}

func mapServiceErr(err error) error {
	switch {
	case errors.Is(err, ErrInvalidUsername):
		return errutil.NewError(http.StatusBadRequest, "invalid username")
	case errors.Is(err, ErrPasswordTooWeak):
		return errutil.NewError(http.StatusBadRequest, "password too weak (require at least Moderate)")
	case errors.Is(err, ErrDuplicateName):
		return errutil.ErrDuplicateName
	case errors.Is(err, ErrUnauthorized):
		return errutil.ErrUnauthorized
	case errors.Is(err, ErrUserNotFound):
		return errutil.ErrUserNotFound
	case errors.Is(err, ErrInvalidUID):
		return errutil.ErrUnauthorized
	default:
		return errutil.ErrInternalServer
	}
}
