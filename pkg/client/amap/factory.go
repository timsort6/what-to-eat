package amap

import (
	"fmt"
	"strings"
	"time"
)

// NewFromOptions builds an SDK client from repo config options.
func NewFromOptions(o *Options) (*Client, error) {
	if o == nil {
		return nil, fmt.Errorf("amap: nil options")
	}
	key := strings.TrimSpace(o.Key)
	if key == "" {
		return nil, fmt.Errorf("amap: empty key")
	}

	var opts []Option
	if strings.TrimSpace(o.BaseURL) != "" {
		opts = append(opts, WithBaseURL(strings.TrimSpace(o.BaseURL)))
	}
	if o.TimeoutSeconds > 0 {
		opts = append(opts, WithTimeout(time.Duration(o.TimeoutSeconds)*time.Second))
	}

	return NewClient(key, opts...), nil
}
