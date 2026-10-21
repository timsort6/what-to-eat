package addresses

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"food-agent/pkg/model"
	"food-agent/pkg/server/encoding"
	"food-agent/pkg/server/errutil"
	"food-agent/pkg/token"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc Service
}

func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Create(c *gin.Context) {
	userID, ok := mustUserID(c)
	if !ok {
		return
	}

	var req CreateAddressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		encoding.HandleError(c, errutil.ErrJSONFormat)
		return
	}
	req.Label = strings.TrimSpace(req.Label)
	req.Location = strings.TrimSpace(req.Location)
	if req.Label == "" || req.Location == "" {
		encoding.HandleError(c, errutil.ErrIllegalParameter)
		return
	}

	addr, err := h.svc.Create(c.Request.Context(), userID, req)
	if err != nil {
		encoding.HandleError(c, mapServiceErr(err))
		return
	}
	encoding.HandleSuccess(c, toDTO(*addr))
}

func (h *Handler) List(c *gin.Context) {
	userID, ok := mustUserID(c)
	if !ok {
		return
	}

	addrs, err := h.svc.List(c.Request.Context(), userID)
	if err != nil {
		encoding.HandleError(c, errutil.ErrInternalServer)
		return
	}
	out := make([]Address, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, toDTO(a))
	}
	encoding.HandleSuccessList(c, int64(len(out)), out)
}

func (h *Handler) Update(c *gin.Context) {
	userID, ok := mustUserID(c)
	if !ok {
		return
	}

	id, err := parseUintParam(c.Param("id"))
	if err != nil || id == 0 {
		encoding.HandleError(c, errutil.ErrIllegalParameter)
		return
	}

	var req UpdateAddressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		encoding.HandleError(c, errutil.ErrJSONFormat)
		return
	}
	if req.Label != nil {
		v := strings.TrimSpace(*req.Label)
		if v == "" {
			encoding.HandleError(c, errutil.ErrIllegalParameter)
			return
		}
		req.Label = &v
	}
	if req.Location != nil {
		v := strings.TrimSpace(*req.Location)
		if v == "" {
			encoding.HandleError(c, errutil.ErrIllegalParameter)
			return
		}
		req.Location = &v
	}

	addr, err := h.svc.Update(c.Request.Context(), userID, uint(id), req)
	if err != nil {
		encoding.HandleError(c, mapServiceErr(err))
		return
	}
	encoding.HandleSuccess(c, toDTO(*addr))
}

func (h *Handler) Delete(c *gin.Context) {
	userID, ok := mustUserID(c)
	if !ok {
		return
	}

	id, err := parseUintParam(c.Param("id"))
	if err != nil || id == 0 {
		encoding.HandleError(c, errutil.ErrIllegalParameter)
		return
	}

	if err := h.svc.Delete(c.Request.Context(), userID, uint(id)); err != nil {
		encoding.HandleError(c, mapServiceErr(err))
		return
	}
	encoding.HandleSuccess(c, map[string]any{"deleted_id": id})
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

func parseUintParam(s string) (uint64, error) {
	return strconv.ParseUint(strings.TrimSpace(s), 10, 64)
}

func mapServiceErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return errutil.ErrNotFound
	}
	if errors.Is(err, ErrPermissionDenied) {
		return errutil.ErrPermissionDenied
	}
	// Map validation-ish errors to 400.
	msg := strings.TrimSpace(err.Error())
	if strings.Contains(msg, "location") || strings.Contains(msg, "label") || strings.Contains(msg, "lng") || strings.Contains(msg, "lat") {
		return errutil.NewError(http.StatusBadRequest, msg)
	}
	return errutil.ErrInternalServer
}

func toDTO(a model.UserFavoriteAddress) Address {
	var raw json.RawMessage
	if strings.TrimSpace(a.RawPOIJSON) != "" && json.Valid([]byte(a.RawPOIJSON)) {
		raw = json.RawMessage([]byte(a.RawPOIJSON))
	}
	return Address{
		ID:        a.ID,
		Label:     a.Label,
		IsDefault: a.IsDefault,

		AmapID:   a.AmapID,
		Name:     a.Name,
		Type:     a.Type,
		TypeCode: a.TypeCode,
		Tel:      a.Tel,
		Tag:      a.Tag,

		FormattedAddress: a.FormattedAddress,
		Address:          a.Address,

		Location:  a.Location,
		Longitude: a.Longitude,
		Latitude:  a.Latitude,

		Pname:    a.Pname,
		Cityname: a.Cityname,
		Adname:   a.Adname,
		Citycode: a.Citycode,
		Adcode:   a.Adcode,

		AMapPOI: raw,
	}
}
