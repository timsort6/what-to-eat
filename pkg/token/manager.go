package token

import (
	"context"
	"time"
)

// Info describes a user that has been authenticated to the system.
type Info struct {
	UID      string `json:"uid,omitempty"`
	Username string `json:"username,omitempty"`
	Name     string `json:"name,omitempty"`
	Primary  bool   `json:"primary,omitempty"`
	// DataAuth []int64 `json:"data_auth,omitempty"`
}

// Manager issues token to user and verify token
type Manager interface {
	// IssueTo issues a token a User, return error if issuing process failed
	IssueTo(info Info, expiresIn time.Duration) (string, error)

	// Verify verifies a token, and return a user info if it's a valid token, otherwise return error
	Verify(string) (Info, error)

	// GetTokenFromCtx extracts the token from the given context.
	// It returns the token string if found, otherwise returns an error.
	GetTokenFromCtx(ctx context.Context) (string, error)
}
