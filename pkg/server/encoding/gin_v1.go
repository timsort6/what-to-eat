package encoding

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"food-agent/pkg/server/errutil"
)

const (
	apiVersionV1 = "v1"
)

type response struct {
	ApiVersion string `json:"api_version"`
	Code       int    `json:"code"`
	Message    string `json:"message"`
	Data       any    `json:"data,omitempty"`
}

type commonList struct {
	PageToken string `json:"page_token,omitempty"`
	Total     int64  `json:"total"`
	Items     any    `json:"items"`
}

func HandleSuccess(c *gin.Context, data ...any) {
	res := response{
		ApiVersion: apiVersionV1,
		Code:       200,
		Message:    "success",
		Data:       nil,
	}

	if len(data) > 0 {
		res.Data = data[0]
	}

	c.JSON(http.StatusOK, res)
}

func HandleSuccessList(c *gin.Context, total int64, items any) {
	HandleSuccess(c, commonList{Total: total, Items: items})
}

func HandleSuccessTokenList(c *gin.Context, pageToken string, items any) {
	HandleSuccess(c, commonList{PageToken: pageToken, Items: items})
}

func HandleSuccessRawJSON(c *gin.Context, message json.RawMessage) {
	HandleSuccess(c, message)
}

// HandleError detects proper status code, then write it and logs error.
func HandleError(c *gin.Context, err error) {
	var serviceErr errutil.ServiceError
	switch t := err.(type) {
	case errutil.ServiceError:
		serviceErr = t
	default:
		zap.L().Error("unexpected error", zap.Error(err))
		serviceErr = errutil.ErrInternalServer
	}

	handleError(c, serviceErr)
}

func handleError(c *gin.Context, err errutil.ServiceError) {
	_, fn, line, _ := runtime.Caller(2)
	zap.L().Error(fmt.Sprintf("%s:%d", fn, line), zap.Error(err))
	c.AbortWithStatusJSON(err.Code, response{ApiVersion: apiVersionV1, Code: err.Code,
		Message: err.Message, Data: err.Data})
}
