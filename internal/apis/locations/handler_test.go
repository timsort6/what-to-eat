package locations

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"food-agent/pkg/server/encoding"
	"food-agent/pkg/server/errutil"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func fixtureRouter(t *testing.T, upstream AmapMCP) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject: "1", Issuer: "location-fixture", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}).SignedString(key)
	require.NoError(t, err)
	authenticate := func(c *gin.Context) {
		bearer := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		parsed, err := jwt.Parse(bearer, func(token *jwt.Token) (any, error) {
			return key, nil
		}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("location-fixture"), jwt.WithExpirationRequired())
		if err != nil || !parsed.Valid {
			encoding.HandleError(c, errutil.ErrUnauthorized)
			return
		}
		c.Next()
	}
	svc, err := NewService(upstream)
	require.NoError(t, err)
	router := gin.New()
	require.NoError(t, RegisterRoutes(router.Group("/api/v1"), Deps{Service: svc, Authenticate: authenticate}))
	return router, signed
}

func TestHandler_rejectsBeforeUpstream_whenAuthOrQueryIsInvalid(t *testing.T) {
	cases := []struct {
		name   string
		query  string
		auth   bool
		status int
	}{
		{"missing authentication", "query=station", false, 401},
		{"missing query", "", true, 400},
		{"blank query", "query=%20%20", true, 400},
		{"query over limit", "query=" + strings.Repeat("a", 81), true, 400},
		{"unicode over limit", "query=" + url.QueryEscape(strings.Repeat("站", 81)), true, 400},
		{"city over limit", "query=station&city=" + strings.Repeat("b", 101), true, 400},
		{"duplicate query", "query=one&query=two", true, 400},
		{"duplicate city", "query=station&city=one&city=two", true, 400},
		{"tool injection", "query=station&tool=amap.regeo", true, 400},
		{"URL injection", "query=station&url=https%3A%2F%2Finvalid.example", true, 400},
		{"arbitrary parameters", "query=station&page_size=99", true, 400},
		{"coordinate injection", "query=station&location=NaN,31", true, 400},
		{"malformed escape", "query=%zz", true, 400},
		{"malformed UTF8", "query=%FF", true, 400},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			router, bearer := fixtureRouter(t, scriptedMCP(func(ctx context.Context, name string, args map[string]any) (string, error) {
				calls++
				return "", ErrUpstream
			}))
			req := httptest.NewRequest(http.MethodGet, "/api/v1/locations/search?"+scenario.query, nil)
			if scenario.auth {
				req.Header.Set("Authorization", "Bearer "+bearer)
			}
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			require.Equal(t, scenario.status, recorder.Code)
			require.Equal(t, 0, calls)
		})
	}
}

func TestHandler_projectsOnlyPublicFields_whenSearchSucceeds(t *testing.T) {
	calls := 0
	router, bearer := fixtureRouter(t, scriptedMCP(func(ctx context.Context, name string, args map[string]any) (string, error) {
		calls++
		require.Equal(t, strings.Repeat("站", 80), args["keywords"])
		require.Equal(t, strings.Repeat("城", 100), args["region"])
		return `{"status":"1","pois":[{"id":"station-1","name":"station","location":"121,31","address":[],"cityname":null,"key":"private-marker","url":"private-marker","raw":{"secret":"private-marker"}}]}`, nil
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/locations/search?query="+
		url.QueryEscape(" "+strings.Repeat("站", 80)+" ")+"&city="+url.QueryEscape(" "+strings.Repeat("城", 100)+" "), nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	require.Equal(t, 200, recorder.Code)
	var response struct {
		Data struct {
			Total int64                        `json:"total"`
			Items []map[string]json.RawMessage `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.EqualValues(t, 1, response.Data.Total)
	require.Len(t, response.Data.Items, 1)
	require.Len(t, response.Data.Items[0], 5)
	for _, field := range []string{"id", "name", "address", "location", "city"} {
		require.Contains(t, response.Data.Items[0], field)
	}
	require.NotContains(t, recorder.Body.String(), "private-marker")
	require.Equal(t, 1, calls)
}

func TestHandler_returnsControlledStatus_whenSearchFails(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		err    error
		status int
	}{
		{"upstream failure", errors.New("private-marker tool URL credential"), 502},
		{"deadline", context.DeadlineExceeded, 504},
		{"cancellation", context.Canceled, 408},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			router, bearer := fixtureRouter(t, scriptedMCP(func(ctx context.Context, name string, args map[string]any) (string, error) {
				calls++
				return "", scenario.err
			}))
			req := httptest.NewRequest(http.MethodGet, "/api/v1/locations/search?query=station", nil)
			req.Header.Set("Authorization", "Bearer "+bearer)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			require.Equal(t, scenario.status, recorder.Code)
			require.NotContains(t, recorder.Body.String(), "private-marker")
			require.NotContains(t, recorder.Body.String(), "\"items\"")
			require.Equal(t, 1, calls)
		})
	}
}

func TestRegistration_rejectsMissingDependencies(t *testing.T) {
	require.ErrorIs(t, RegisterRoutes(gin.New(), Deps{}), errDependency)
	_, err := NewService(nil)
	require.ErrorIs(t, err, errDependency)
}

func TestHandler_rejectsInvalidToken_withoutCallingUpstream(t *testing.T) {
	calls := 0
	router, _ := fixtureRouter(t, scriptedMCP(func(ctx context.Context, name string, args map[string]any) (string, error) {
		calls++
		return "", ErrUpstream
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/locations/search?query=station", nil)
	req.Header.Set("Authorization", "Bearer invalid-fixture-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Equal(t, 0, calls)
}
