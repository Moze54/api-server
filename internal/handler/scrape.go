// 爬虫接口：POST /api/v1/fetch/:name
// 流程：缓存命中直接返回 → 信号量闸门 → 抓取解析 → 写缓存 → 返回。
package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/gin-gonic/gin"

	"api-server/internal/cache"
	"api-server/internal/provider"
	"api-server/internal/resp"
	"api-server/internal/scraper"
)

// Scrape 爬虫路由处理器。
func Scrape(reg *scraper.Registry, pool *scraper.Pool, ttlCache *cache.Cache, ttl time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		sp, ok := reg.Get(name)
		if !ok {
			resp.Fail(c, resp.CodeNotFound, "未知爬虫: "+name)
			return
		}
		body, err := readBody(c)
		if err != nil {
			return // readBody 已写响应
		}
		c.Set("log_target", name)

		// 缓存键 = 爬虫名 + 完整请求体；实现 NoCache 的随机类爬虫跳过缓存
		_, noCache := sp.(scraper.NoCache)
		sum := sha256.Sum256(append([]byte(name), body...))
		cacheKey := hex.EncodeToString(sum[:])
		if !noCache {
			if payload, hit, err := ttlCache.Get(c.Request.Context(), cacheKey); err == nil && hit {
				c.Set("cache_hit", true)
				c.Header("X-Cache", "hit")
				resp.OK(c, json.RawMessage(payload))
				return
			}
		}

		var result any
		err = pool.Run(c.Request.Context(), func(ctx context.Context) error {
			var e error
			result, e = sp.Run(ctx, body)
			return e
		})
		if err != nil {
			mapTaskError(c, err)
			return
		}

		data, err := json.Marshal(result)
		if err != nil {
			resp.Fail(c, resp.CodeInternal, "结果序列化失败")
			return
		}
		// 写缓存：L1 同步微秒级，L2 异步落盘（规范 §1.2）；失败不影响本次响应
		// 实现 CacheTTL 的爬虫用专属 TTL（行情类 60s），否则用全局默认
		if !noCache {
			useTTL := ttl
			if t, ok := sp.(scraper.CacheTTL); ok {
				useTTL = t.CacheTTL()
			}
			_ = ttlCache.Set(cacheKey, string(data), useTTL)
			c.Header("X-Cache", "miss")
		} else {
			c.Header("X-Cache", "none")
		}
		resp.OK(c, json.RawMessage(data))
	}
}

// readBody 读取请求体（上限 1MB），失败时已写好响应。
func readBody(c *gin.Context) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		resp.Fail(c, resp.CodeBadRequest, "读取请求体失败")
		return nil, err
	}
	if len(body) == 0 {
		resp.Fail(c, resp.CodeBadRequest, "请求体不能为空（JSON）")
		return nil, errors.New("empty body")
	}
	return body, nil
}

// mapTaskError 把池/任务错误映射为统一响应。
// scrape 与 vendor 共用。
func mapTaskError(c *gin.Context, err error) {
	var sErr *scraper.Error
	var pErr *provider.Error
	switch {
	case errors.As(err, &sErr):
		resp.Fail(c, sErr.Code, sErr.Message)
	case errors.As(err, &pErr):
		resp.Fail(c, pErr.Code, pErr.Message)
	case errors.Is(err, scraper.ErrBusy):
		resp.Fail(c, resp.CodeBusy, "任务并发已满，稍后重试")
	case errors.Is(err, context.DeadlineExceeded):
		resp.Fail(c, resp.CodeUpstreamTimeout, "任务超时")
	case errors.Is(err, context.Canceled):
		resp.Fail(c, resp.CodeBadRequest, "请求已取消")
	default:
		resp.Fail(c, resp.CodeInternal, "内部错误")
	}
}
