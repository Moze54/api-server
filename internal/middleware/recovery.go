// 中间件：请求 ID 与 panic 恢复。
// 链路约定（在 main 中按此顺序注册）：
//	RequestID -> AccessLog -> Recovery -> Auth -> RateLimit
// AccessLog 在最外层用 defer 记录日志，Recovery 在其内层捕获 panic，
// 保证 panic 也能以 500 状态被记入用量日志。
package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"api-server/internal/resp"
)

// RequestID 为每个请求生成/透传 request id。
// 客户端传入的 id 只在合法时采信（≤64 位，仅 hex 与 '-'），防止 oversized/注入。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if !validRequestID(id) {
			id = newRequestID()
		}
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func validRequestID(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F', r == '-':
		default:
			return false
		}
	}
	return true
}

func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Recovery 捕获 handler 中的 panic，统一返回 50000，避免单个接口拖垮进程。
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic recovered",
					"request_id", c.GetString("request_id"),
					"path", c.Request.URL.Path,
					"err", r,
					"stack", string(debug.Stack()),
				)
				c.Set(resp.CtxBizCode, resp.CodeInternal)
				c.AbortWithStatusJSON(500, resp.Body{
					Code:      resp.CodeInternal,
					Message:   "内部错误",
					RequestID: c.GetString("request_id"),
				})
			}
		}()
		c.Next()
	}
}
