package amap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// Client is a lightweight client for AMap Web Service APIs (based on resty).
//
// It supports both v3 and v5 APIs by deriving base urls from a single base url
// configured by the app (see WithBaseURL).
type Client struct {
	key string

	baseRoot string // e.g. https://restapi.amap.com
	baseV3   string // e.g. https://restapi.amap.com/v3
	baseV5   string // e.g. https://restapi.amap.com/v5

	resty *resty.Client
}

type Option func(c *Client)

// WithBaseURL sets base url. It may be a root url or a versioned url:
// - https://restapi.amap.com
// - https://restapi.amap.com/v3
// - https://restapi.amap.com/v5
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		root := deriveRoot(baseURL)
		c.baseRoot = root
		c.baseV3 = strings.TrimRight(root, "/") + "/v3"
		c.baseV5 = strings.TrimRight(root, "/") + "/v5"
		// Keep resty base on v3, because most existing endpoints are v3 and
		// v5 endpoints will be called with absolute urls.
		c.resty.SetBaseURL(c.baseV3)
	}
}

// WithTimeout sets request timeout (default 5s).
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.resty.SetTimeout(timeout)
	}
}

// NewClient creates a new AMap client.
func NewClient(key string, opts ...Option) *Client {
	r := resty.New().
		SetBaseURL("https://restapi.amap.com/v3").
		SetTimeout(5 * time.Second)

	c := &Client{
		key:      key,
		baseRoot: "https://restapi.amap.com",
		baseV3:   "https://restapi.amap.com/v3",
		baseV5:   "https://restapi.amap.com/v5",
		resty:    r,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) Place() *PlaceService {
	return &PlaceService{client: c}
}

func (c *Client) Geo() *GeoService {
	return &GeoService{client: c}
}

func (c *Client) POI2() *POI2Service {
	return &POI2Service{client: c}
}

func (c *Client) doGetV3(ctx context.Context, path string, query map[string]string, out any) error {
	if query == nil {
		query = make(map[string]string)
	}
	query["key"] = c.key

	resp, err := c.resty.R().
		SetContext(ctx).
		SetQueryParams(query).
		Get(path)
	if err != nil {
		return fmt.Errorf("amap http get %s: %w", path, err)
	}
	if resp.IsError() {
		return fmt.Errorf("amap http status %d: %s", resp.StatusCode(), string(resp.Body()))
	}
	if err := json.Unmarshal(resp.Body(), out); err != nil {
		return fmt.Errorf("amap unmarshal response: %w", err)
	}
	return nil
}

func (c *Client) doGetAbs(ctx context.Context, fullURL string, query map[string]string, out any) error {
	if query == nil {
		query = make(map[string]string)
	}
	query["key"] = c.key

	resp, err := c.resty.R().
		SetContext(ctx).
		SetQueryParams(query).
		Get(fullURL)
	if err != nil {
		return fmt.Errorf("amap http get %s: %w", fullURL, err)
	}
	if resp.IsError() {
		return fmt.Errorf("amap http status %d: %s", resp.StatusCode(), string(resp.Body()))
	}
	if err := json.Unmarshal(resp.Body(), out); err != nil {
		return fmt.Errorf("amap unmarshal response: %w", err)
	}
	return nil
}

func (c *Client) v5URL(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimRight(c.baseV5, "/") + path
}

func deriveRoot(baseURL string) string {
	s := strings.TrimSpace(baseURL)
	s = strings.TrimRight(s, "/")
	if s == "" {
		return "https://restapi.amap.com"
	}

	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	u.RawQuery = ""
	u.Fragment = ""

	p := strings.TrimRight(u.Path, "/")
	switch {
	case strings.HasSuffix(p, "/v3"):
		u.Path = strings.TrimSuffix(p, "/v3")
	case strings.HasSuffix(p, "/v5"):
		u.Path = strings.TrimSuffix(p, "/v5")
	default:
		u.Path = p
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return strings.TrimRight(u.String(), "/")
}
