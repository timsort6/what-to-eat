package amap

import (
	sdk "food-agent/pkg/amap"
	"time"
)

// Client is an alias to the actual SDK client in `food-agent/pkg/amap`.
type Client = sdk.Client

type Option = sdk.Option

// WithBaseURL sets base url. It may be a root url or a versioned url:
// - https://restapi.amap.com
// - https://restapi.amap.com/v3
// - https://restapi.amap.com/v5
func WithBaseURL(baseURL string) Option {
	return sdk.WithBaseURL(baseURL)
}

// WithTimeout sets request timeout (default 5s).
func WithTimeout(timeout time.Duration) Option {
	return sdk.WithTimeout(timeout)
}

// NewClient creates an AMap SDK client.
func NewClient(key string, opts ...Option) *Client {
	return sdk.NewClient(key, opts...)
}
