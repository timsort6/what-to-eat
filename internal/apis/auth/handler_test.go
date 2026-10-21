package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"food-agent/pkg/model"
	"food-agent/pkg/token"
	"food-agent/pkg/utils/pwdutil"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type memUserStore struct {
	nextID uint
	byID   map[uint]*model.User
	byName map[string]*model.User
}

func newMemUserStore() *memUserStore {
	return &memUserStore{
		nextID: 1,
		byID:   make(map[uint]*model.User),
		byName: make(map[string]*model.User),
	}
}

func (s *memUserStore) Create(ctx context.Context, user *model.User) error {
	_ = ctx
	user.ID = s.nextID
	s.nextID++
	cp := *user
	s.byID[cp.ID] = &cp
	s.byName[cp.Username] = &cp
	return nil
}

func (s *memUserStore) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	_ = ctx
	if u := s.byName[username]; u != nil {
		cp := *u
		return &cp, nil
	}
	return nil, ErrNotFound
}

func (s *memUserStore) GetByID(ctx context.Context, id uint) (*model.User, error) {
	_ = ctx
	if u := s.byID[id]; u != nil {
		cp := *u
		return &cp, nil
	}
	return nil, ErrNotFound
}

type apiResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func TestRegisterLoginMe(t *testing.T) {
	gin.SetMode(gin.TestMode)

	users := newMemUserStore()
	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256, token.SetIssuer("food-agent"))

	r := gin.New()
	v1 := r.Group("/api/v1")
	if err := RegisterRoutes(v1, Deps{
		Users:             users,
		Tokens:            mgr,
		JWTExpiresSeconds: 3600,
	}); err != nil {
		t.Fatalf("register routes: %v", err)
	}

	// register
	reg := RegisterRequest{Username: "test_user", Password: "Abcd1234!"}
	body, _ := json.Marshal(reg)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
	}

	// login
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", w.Code, w.Body.String())
	}
	var resp apiResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var lr LoginResponse
	if err := json.Unmarshal(resp.Data, &lr); err != nil {
		t.Fatalf("unmarshal login data: %v", err)
	}
	if lr.Token == "" {
		t.Fatalf("expected token")
	}

	// me without token -> 401
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("me without token status=%d body=%s", w.Code, w.Body.String())
	}

	// me with token -> 200
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+lr.Token)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRegisterRejectsWeakPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := newMemUserStore()
	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256)

	r := gin.New()
	v1 := r.Group("/api/v1")
	if err := RegisterRoutes(v1, Deps{Users: users, Tokens: mgr, JWTExpiresSeconds: 3600}); err != nil {
		t.Fatalf("register routes: %v", err)
	}

	// Ensure chosen password is actually below Moderate in case pwdutil changes.
	weak := "123456"
	if pwdutil.CheckPasswordRating(weak, pwdutil.Moderate) {
		t.Fatalf("test password unexpectedly rated Moderate")
	}

	reg := RegisterRequest{Username: "test_user2", Password: weak}
	body, _ := json.Marshal(reg)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestLoginExpires(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := newMemUserStore()
	hash, _ := pwdutil.PasswordHash("Abcd1234!")
	_ = users.Create(context.Background(), &model.User{Username: "u11", PasswordHash: hash})

	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256)
	h, err := NewHandler(Deps{Users: users, Tokens: mgr, JWTExpiresSeconds: 1})
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}

	// issue token via handler
	r := gin.New()
	r.POST("/login", h.Login)
	body, _ := json.Marshal(LoginRequest{Username: "u11", Password: "Abcd1234!"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", w.Code, w.Body.String())
	}

	var resp apiResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	var lr LoginResponse
	_ = json.Unmarshal(resp.Data, &lr)

	// Wait for expiration and ensure Verify fails (best-effort).
	time.Sleep(2 * time.Second)
	if _, err := mgr.Verify(lr.Token); err == nil {
		t.Fatalf("expected token to be expired")
	}
}
