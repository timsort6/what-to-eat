package addresses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"food-agent/pkg/token"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type apiResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func TestAddresses_UnauthorizedAndBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	s := &service{dao: newMemFavoriteAddressDAO()}
	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256, token.SetIssuer("food-agent"))

	r := gin.New()
	v1 := r.Group("/api/v1")
	if err := RegisterRoutes(v1, Deps{Service: s, Tokens: mgr}); err != nil {
		t.Fatalf("register routes: %v", err)
	}

	// no token -> 401
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/addresses", bytes.NewReader([]byte(`{"label":"家","location":"121,31"}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}

	// with token but missing required field -> 400
	tok, _ := mgr.IssueTo(token.Info{UID: "1", Username: "u1"}, time.Hour)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/addresses", bytes.NewReader([]byte(`{"label":"家"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAddresses_ForbiddenOnOtherUsersResource(t *testing.T) {
	gin.SetMode(gin.TestMode)

	s := &service{dao: newMemFavoriteAddressDAO()}
	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256, token.SetIssuer("food-agent"))

	r := gin.New()
	v1 := r.Group("/api/v1")
	if err := RegisterRoutes(v1, Deps{Service: s, Tokens: mgr}); err != nil {
		t.Fatalf("register routes: %v", err)
	}

	tok1, _ := mgr.IssueTo(token.Info{UID: "1", Username: "u1"}, time.Hour)
	tok2, _ := mgr.IssueTo(token.Info{UID: "2", Username: "u2"}, time.Hour)

	// create address for user 1
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/addresses", bytes.NewReader([]byte(`{"label":"家","location":"121,31"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok1)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var resp apiResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var created Address
	if err := json.Unmarshal(resp.Data, &created); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("expected id")
	}

	// user 2 tries to update user 1's address -> 403
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/addresses/%d", created.ID), bytes.NewReader([]byte(`{"is_default":true}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok2)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}
