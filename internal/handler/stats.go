// 用量统计接口：GET /api/v1/stats?days=7（仅 admin 密钥）。
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"api-server/internal/resp"
	"api-server/internal/store"
)

type routeRow struct {
	Route     string  `json:"route"`
	Count     int64   `json:"count"`
	CacheHits int64   `json:"cache_hits"`
	AvgLatency float64 `json:"avg_latency_ms"`
}

type keyRow struct {
	KeyName    string  `json:"key_name"`
	Count      int64   `json:"count"`
	AvgLatency float64 `json:"avg_latency_ms"`
}

type dailyRow struct {
	Date   string `json:"date"`
	Count  int64  `json:"count"`
	Errors int64  `json:"errors"`
}

// Stats 按时间窗聚合调用量、错误率、延迟与缓存命中。
func Stats(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		days := 7
		if d, err := strconv.Atoi(c.Query("days")); err == nil && d > 0 && d <= 90 {
			days = d
		}
		since := time.Now().AddDate(0, 0, -days).Unix()

		var total, errCount int64
		var avgLatency float64
		err := st.DB.QueryRowContext(c.Request.Context(),
			`SELECT COUNT(*),
			        COALESCE(SUM(CASE WHEN code <> 0 THEN 1 ELSE 0 END), 0),
			        COALESCE(AVG(latency_ms), 0)
			 FROM usage_logs WHERE ts >= ?`, since).
			Scan(&total, &errCount, &avgLatency)
		if err != nil {
			resp.Fail(c, resp.CodeInternal, "查询总量失败")
			return
		}

		routes := []routeRow{}
		rows, err := st.DB.QueryContext(c.Request.Context(),
			`SELECT route, COUNT(*), COALESCE(SUM(cache_hit), 0), COALESCE(AVG(latency_ms), 0)
			 FROM usage_logs WHERE ts >= ? GROUP BY route ORDER BY COUNT(*) DESC`, since)
		if err == nil {
			for rows.Next() {
				var r routeRow
				if rows.Scan(&r.Route, &r.Count, &r.CacheHits, &r.AvgLatency) == nil {
					routes = append(routes, r)
				}
			}
			rows.Close()
		}

		keys := []keyRow{}
		rows, err = st.DB.QueryContext(c.Request.Context(),
			`SELECT key_name, COUNT(*), COALESCE(AVG(latency_ms), 0)
			 FROM usage_logs WHERE ts >= ? GROUP BY key_name ORDER BY COUNT(*) DESC`, since)
		if err == nil {
			for rows.Next() {
				var k keyRow
				if rows.Scan(&k.KeyName, &k.Count, &k.AvgLatency) == nil {
					keys = append(keys, k)
				}
			}
			rows.Close()
		}

		daily := []dailyRow{}
		rows, err = st.DB.QueryContext(c.Request.Context(),
			`SELECT date(ts, 'unixepoch', 'localtime') AS d, COUNT(*),
			        COALESCE(SUM(CASE WHEN code <> 0 THEN 1 ELSE 0 END), 0)
			 FROM usage_logs WHERE ts >= ? GROUP BY d ORDER BY d`, since)
		if err == nil {
			for rows.Next() {
				var dr dailyRow
				if rows.Scan(&dr.Date, &dr.Count, &dr.Errors) == nil {
					daily = append(daily, dr)
				}
			}
			rows.Close()
		}

		c.JSON(http.StatusOK, gin.H{
			"days":      days,
			"total":     total,
			"errors":    errCount,
			"avg_latency_ms": avgLatency,
			"by_route":  routes,
			"by_key":    keys,
			"daily":     daily,
		})
	}
}
