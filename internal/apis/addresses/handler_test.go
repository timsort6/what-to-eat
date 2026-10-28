package addresses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"food-agent/pkg/model"
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

func TestAddresses_CreateRejectsOverlongNameAndAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)

	s := &service{dao: newMemFavoriteAddressDAO()}
	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256, token.SetIssuer("food-agent"))
	r := gin.New()
	if err := RegisterRoutes(r.Group("/api/v1"), Deps{Service: s, Tokens: mgr}); err != nil {
		t.Fatalf("register routes: %v", err)
	}
	tok, _ := mgr.IssueTo(token.Info{UID: "1", Username: "u1"}, time.Hour)

	// 多字节字符验证按字符数（码点）而非字节数校验，对应列宽 name 128 / address 255。
	cases := []struct {
		name, poiName, address string
		want                   int
	}{
		{"name 128 ok", strings.Repeat("文", 128), "", http.StatusOK},
		{"name 129 rejected", strings.Repeat("文", 129), "", http.StatusBadRequest},
		{"address 255 ok", "", strings.Repeat("文", 255), http.StatusOK},
		{"address 256 rejected", "", strings.Repeat("文", 256), http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"label": "家", "location": "121,31", "name": tc.poiName, "address": tc.address})
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/addresses", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+tok)
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status=%d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
			var resp apiResp
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if tc.want == http.StatusBadRequest && (resp.Code != http.StatusBadRequest || !strings.Contains(resp.Message, "too long")) {
				t.Fatalf("error body = %s", w.Body.String())
			}
		})
	}
}

func TestAddresses_UpdateRejectsOverlongFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	s := &service{dao: newMemFavoriteAddressDAO()}
	mgr := token.NewJWTTokenManager([]byte("fake-secret"), jwt.SigningMethodHS256, token.SetIssuer("food-agent"))
	r := gin.New()
	if err := RegisterRoutes(r.Group("/api/v1"), Deps{Service: s, Tokens: mgr}); err != nil {
		t.Fatalf("register routes: %v", err)
	}
	tok, _ := mgr.IssueTo(token.Info{UID: "1", Username: "u1"}, time.Hour)

	do := func(method, path string, body any) (*httptest.ResponseRecorder, apiResp) {
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		r.ServeHTTP(w, req)
		var resp apiResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
		}
		return w, resp
	}

	w, resp := do(http.MethodPost, "/api/v1/addresses", map[string]any{"label": "家", "location": "121,31"})
	if w.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var created Address
	if err := json.Unmarshal(resp.Data, &created); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	path := fmt.Sprintf("/api/v1/addresses/%d", created.ID)

	// 多字节字符按码点计数；前后空白不计入（校验去空白后的入库值）。
	cases := []struct {
		name  string
		field string
		value string
		want  int
	}{
		{"name 128 ok", "name", " " + strings.Repeat("文", 128) + " ", http.StatusOK},
		{"name 129 rejected", "name", strings.Repeat("文", 129), http.StatusBadRequest},
		{"address 255 ok", "address", strings.Repeat("文", 255), http.StatusOK},
		{"address 256 rejected", "address", strings.Repeat("文", 256), http.StatusBadRequest},
		{"label 64 ok", "label", strings.Repeat("家", 64), http.StatusOK},
		{"label 65 rejected", "label", strings.Repeat("家", 65), http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, resp := do(http.MethodPut, path, map[string]any{tc.field: tc.value})
			if w.Code != tc.want {
				t.Fatalf("status=%d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
			if tc.want == http.StatusBadRequest && (resp.Code != http.StatusBadRequest || !strings.Contains(resp.Message, "too long")) {
				t.Fatalf("error body = %s", w.Body.String())
			}
		})
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

func TestToDTO_MapsFavoriteAddressContract(t *testing.T) {
	addr := model.UserFavoriteAddress{
		ID:               7,
		Label:            "家",
		IsDefault:        true,
		AmapID:           "poi-1",
		Name:             "餐馆",
		Type:             "餐饮服务",
		TypeCode:         "050000",
		Tel:              "123456",
		Tag:              "面馆",
		FormattedAddress: "上海市黄浦区人民路1号",
		Address:          "人民路1号",
		Location:         "121.5,31.2",
		Longitude:        121.5,
		Latitude:         31.2,
		Pname:            "上海市",
		Cityname:         "上海市",
		Adname:           "黄浦区",
		Citycode:         "021",
		Adcode:           "310101",
		RawPOIJSON:       `{"id":"poi-1","name":"餐馆"}`,
	}

	got := toDTO(addr)
	if got.ID != addr.ID || got.Label != addr.Label || !got.IsDefault {
		t.Fatalf("identity fields = %+v", got)
	}
	if got.AmapID != addr.AmapID || got.Name != addr.Name || got.Type != addr.Type || got.TypeCode != addr.TypeCode || got.Tel != addr.Tel || got.Tag != addr.Tag {
		t.Fatalf("poi fields = %+v", got)
	}
	if got.FormattedAddress != addr.FormattedAddress || got.Address != addr.Address || got.Location != addr.Location || got.Longitude != addr.Longitude || got.Latitude != addr.Latitude {
		t.Fatalf("location fields = %+v", got)
	}
	if got.Pname != addr.Pname || got.Cityname != addr.Cityname || got.Adname != addr.Adname || got.Citycode != addr.Citycode || got.Adcode != addr.Adcode {
		t.Fatalf("administrative fields = %+v", got)
	}
	if string(got.AMapPOI) != addr.RawPOIJSON {
		t.Fatalf("raw poi = %s, want %s", got.AMapPOI, addr.RawPOIJSON)
	}

	addr.RawPOIJSON = "not-json"
	if got := toDTO(addr).AMapPOI; got != nil {
		t.Fatalf("invalid raw poi = %s, want omitted", got)
	}
}
