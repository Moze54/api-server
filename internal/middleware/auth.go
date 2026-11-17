// 中间件：API Key 鉴权。
// 密钥从 X-API-Key 头或 Authorization: Bearer <key> 读取。
package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"api-server/internal/config"
	"api-server/internal/resp"
)

const (
	ctxKeyName  = "auth_key_name"
	ctxKeyAdmin = "auth_key_admin"
)

type Authenticator struct {
	keys map[string]config.KeyConfig // 密钥 -> 配置
}

func NewAuthenticator(cfg config.AuthConfig) *Authenticator {
	m := make(map[string]config.KeyConfig, len(cfg.Keys))
	for _, k := range cfg.Keys {
		m[k.Key] = k
	}
	return &Authenticator{keys: m}
}

// Middleware 校验密钥；成功则把调用方标识与 admin 标记写入 context。
func (a *Authenticator) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader("X-API-Key")
		if key == "" {
			if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
				key = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
			}
		}
		if key == "" {
			resp.Fail(c, resp.CodeInvalidKey, "缺少 API Key（X-API-Key 或 Authorization: Bearer）")
			return
		}
		k, ok := a.keys[key]
		if !ok {
			resp.Fail(c, resp.CodeInvalidKey, "无效 API Key")
			return
		}
		c.Set(ctxKeyName, k.Name)
		c.Set(ctxKeyAdmin, k.Admin)
		c.Next()
	}
}

// KeyName 返回当前请求的调用方标识（未鉴权时为空）。
func KeyName(c *gin.Context) string { return c.GetString(ctxKeyName) }

// IsAdmin 返回当前请求是否为 admin 密钥。
func IsAdmin(c *gin.Context) bool { return c.GetBool(ctxKeyAdmin) }

// RequireAdmin 管理接口守卫，需在 Auth 之后注册。
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsAdmin(c) {
			resp.Fail(c, resp.CodeForbidden, "需要 admin 密钥")
			return
		}
		c.Next()
	}
}
