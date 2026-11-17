// 中间件：访问日志异步写 SQLite（usage_logs 表）。
// 请求 goroutine 只做一次非阻塞入队，落盘由独立 goroutine 完成；
// 关停时 Close() 排空队列，丢弃量会打日志告警。
package middleware

import (
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"api-server/internal/resp"
	"api-server/internal/store"
)

type LogEntry struct {
	Ts        int64
	KeyName   string
	Route     string
	Target    string
	Status    int
	Code      int
	LatencyMs int64
	CacheHit  bool
}

type AccessLog struct {
	store   *store.Store
	logger  *slog.Logger
	ch      chan LogEntry
	done    chan struct{}
	dropped atomic.Int64
}

func NewAccessLog(st *store.Store, logger *slog.Logger) *AccessLog {
	al := &AccessLog{
		store:  st,
		logger: logger,
		ch:     make(chan LogEntry, 1024),
		done:   make(chan struct{}),
	}
	go al.worker()
	return al
}

// worker 批量落盘：攒满 64 条或每 500ms 提交一个事务，降低 SQLite 写放大。
func (al *AccessLog) worker() {
	defer close(al.done)
	const q = `INSERT INTO usage_logs(ts, key_name, route, target, status, code, latency_ms, cache_hit)
VALUES(?, ?, ?, ?, ?, ?, ?, ?)`
	batch := make([]LogEntry, 0, 64)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		tx, err := al.store.DB.Begin()
		if err != nil {
			al.logger.Error("访问日志事务开启失败", "err", err)
			batch = batch[:0]
			return
		}
		for _, e := range batch {
			if _, err := tx.Exec(q, e.Ts, e.KeyName, e.Route, e.Target,
				e.Status, e.Code, e.LatencyMs, boolToInt(e.CacheHit)); err != nil {
				al.logger.Error("写入访问日志失败", "err", err)
			}
		}
		if err := tx.Commit(); err != nil {
			al.logger.Error("访问日志事务提交失败", "err", err)
		}
		batch = batch[:0]
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case e, ok := <-al.ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, e)
			if len(batch) >= 64 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Middleware 需注册在链路最外层（紧随 RequestID），用 defer 保证 panic 也被记录。
func (al *AccessLog) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		defer func() {
			route := c.FullPath()
			if route == "" {
				route = c.Request.URL.Path
			}
			select {
			case al.ch <- LogEntry{
				Ts:        time.Now().Unix(),
				KeyName:   KeyName(c),
				Route:     route,
				Target:    c.GetString("log_target"),
				Status:    c.Writer.Status(),
				Code:      c.GetInt(resp.CtxBizCode),
				LatencyMs: time.Since(start).Milliseconds(),
				CacheHit:  c.GetBool("cache_hit"),
			}:
			default:
				al.dropped.Add(1)
			}
		}()
		c.Next()
	}
}

// Close 排空日志队列后返回；用于优雅关停。
func (al *AccessLog) Close() {
	close(al.ch)
	<-al.done
	if d := al.dropped.Load(); d > 0 {
		al.logger.Warn("部分访问日志因队列满被丢弃", "dropped", d)
	}
}
