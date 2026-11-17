// 中间件：按密钥的内存令牌桶限流。
// 密钥数量少（配置文件里枚举），直接用 map 存桶即可，无外部依赖。
package middleware

import (
	"sync"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"api-server/internal/resp"
)

type RateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	rps      rate.Limit
	burst    int
}

func NewRateLimiter(perKeyRPS float64, burst int) *RateLimiter {
	return &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rps:      rate.Limit(perKeyRPS),
		burst:    burst,
	}
}

func (rl *RateLimiter) get(name string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	l, ok := rl.limiters[name]
	if !ok {
		l = rate.NewLimiter(rl.rps, rl.burst)
		rl.limiters[name] = l
	}
	return l
}

// Middleware 需在 Auth 之后注册（按密钥名限流）。
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		name := KeyName(c)
		if name != "" && !rl.get(name).Allow() {
			resp.Fail(c, resp.CodeRateLimited, "请求过于频繁，稍后重试")
			return
		}
		c.Next()
	}
}
