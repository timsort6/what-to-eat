package errutil

import (
	"net/http"
)

type ServiceError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// NewError returns a ServiceError using the code and reason
func NewError(code int, message string, data ...any) ServiceError {
	err := ServiceError{Code: code, Message: message}
	if len(data) > 0 {
		err.Data = data[0]
	}
	return err
}

// Error returns a text representation of the service error
func (s ServiceError) Error() string {
	return s.Message
}

var (
	ErrInternalServer   = NewError(http.StatusInternalServerError, "internal server error")
	ErrIllegalParameter = NewError(http.StatusBadRequest, "illegal parameter")
	ErrDuplicateName    = NewError(http.StatusBadRequest, "duplicate name")
	ErrUserNotFound     = NewError(http.StatusNotFound, "user not found")
	ErrNotFound         = NewError(http.StatusNotFound, "not found")
	ErrUnauthorized     = NewError(http.StatusUnauthorized, "unauthorized")
	ErrPermissionDenied = NewError(http.StatusForbidden, "permission denied")
	ErrIllegalOperation = NewError(http.StatusBadRequest, "illegal operation ")
	ErrGetAuditLogs     = NewError(http.StatusBadRequest, "get audit logs failed")
	ErrInvalidLicense   = NewError(http.StatusBadRequest, "license invalid")
	ErrJSONFormat       = NewError(http.StatusBadRequest, "json format error")
	ErrFullPool         = NewError(http.StatusForbidden, "full pool for more tasks")
	ErrResourceNotFound = NewError(http.StatusNotFound, "resource not found")
)
