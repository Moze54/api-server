package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"api-server/internal/config"
	"api-server/internal/resp"
)

func newAuthTestRouter() (*gin.Engine, *Authenticator) {
	gin.SetMode(gin.TestMode)
	auth := NewAuthenticator(config.AuthConfig{Keys: []config.KeyConfig{
		{Name: "apihub", Key: "k-user", Admin: false},
		{Name: "admin", Key: "k-admin", Admin: true},
	}})
	r := gin.New()
	r.Use(auth.Middleware())
	r.GET("/admin-only", RequireAdmin(), func(c *gin.Context) {
		resp.OK(c, gin.H{"key_name": KeyName(c)})
	})
	return r, auth
}

func do(t *testing.T, r *gin.Engine, header, value string) (*httptest.ResponseRecorder, resp.Body) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin-only", nil)
	if value != "" {
		req.Header.Set(header, value)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var body resp.Body
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是统一 JSON: %v (%q)", err, w.Body.String())
	}
	return w, body
}

func TestAuthMissingKey(t *testing.T) {
	r, _ := newAuthTestRouter()
	w, body := do(t, r, "", "")
	if w.Code != http.StatusUnauthorized || body.Code != resp.CodeInvalidKey {
		t.Fatalf("缺 key 应 401/40101, got http=%d code=%d", w.Code, body.Code)
	}
}

func TestAuthWrongKey(t *testing.T) {
	r, _ := newAuthTestRouter()
	w, body := do(t, r, "X-API-Key", "bad")
	if w.Code != http.StatusUnauthorized || body.Code != resp.CodeInvalidKey {
		t.Fatalf("错 key 应 401/40101, got http=%d code=%d", w.Code, body.Code)
	}
}

func TestAuthBearerKey(t *testing.T) {
	r, _ := newAuthTestRouter()
	w, body := do(t, r, "Authorization", "Bearer k-admin")
	data, _ := body.Data.(map[string]any)
	if w.Code != http.StatusOK || body.Code != resp.CodeOK || data["key_name"] != "admin" {
		t.Fatalf("Bearer key 应通过且取到调用方标识, got http=%d body=%q", w.Code, w.Body.String())
	}
}

func TestRequireAdmin(t *testing.T) {
	r, _ := newAuthTestRouter()

	w, body := do(t, r, "X-API-Key", "k-user")
	if w.Code != http.StatusForbidden || body.Code != resp.CodeForbidden {
		t.Fatalf("非 admin 应 403/40301, got http=%d code=%d", w.Code, body.Code)
	}

	w, body = do(t, r, "X-API-Key", "k-admin")
	if w.Code != http.StatusOK || body.Code != resp.CodeOK {
		t.Fatalf("admin 应通过, got http=%d code=%d", w.Code, body.Code)
	}
}
