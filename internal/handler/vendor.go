// 厂商接口：POST /api/v1/vendor/:name
// 与爬虫共用同一并发闸门与错误映射；厂商结果不缓存（由适配器自行决定）。
package handler

import (
	"context"

	"github.com/gin-gonic/gin"

	"api-server/internal/provider"
	"api-server/internal/resp"
	"api-server/internal/scraper"
)

// Vendor 厂商路由处理器。
func Vendor(reg *provider.Registry, pool *scraper.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		v, ok := reg.Get(name)
		if !ok {
			resp.Fail(c, resp.CodeNotFound, "未知厂商: "+name)
			return
		}
		body, err := readBody(c)
		if err != nil {
			return
		}
		c.Set("log_target", name)

		var result any
		err = pool.Run(c.Request.Context(), func(ctx context.Context) error {
			var e error
			result, e = v.Call(ctx, body)
			return e
		})
		if err != nil {
			mapTaskError(c, err)
			return
		}
		resp.OK(c, result)
	}
}
