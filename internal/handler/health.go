// Package handler HTTP 路由处理器。
package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"api-server/internal/cache"
	"api-server/internal/scraper"
)

// Health 探活接口，免鉴权；附带运行时指标便于巡检。
func Health(boot time.Time, pool *scraper.Pool, cacheL1 *cache.Cache) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":           "ok",
			"uptime_seconds":   int(time.Since(boot).Seconds()),
			"tasks_in_flight":  pool.InFlight(),
			"cache_l1_entries": cacheL1.L1Size(),
		})
	}
}
