// Package cache 两级 TTL 结果缓存：
//
//	L1 内存 —— 微秒级命中，容量与单条大小受限，进程内；
//	L2 SQLite —— 重启后仍然有效（L1 冷启动时兜底，避免重复抓取）。
//
// 重复请求直接回缓存，既省自身资源，也对被爬站点友好（控频防封）。
package cache

import (
	"context"
	"database/sql"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

var (
	maxL1Entries  = 1024            // L1 最大条目数
	maxL1Payload  = 256 << 10       // 256KB，超过不进 L1（防大响应撑内存）
	maxL2Payload  = 1 << 20         // 1MB，超过不缓存（防 SQLite 膨胀）
	l2CleanPeriod = 10              // 约 1/10 概率顺带清理 L2 过期行
)

type l1Entry struct {
	payload   string
	expiresAt int64 // unix 毫秒
	addedAt   int64 // unix 毫秒，容量满时淘汰最旧
}

// Stats 缓存命中统计快照。
type Stats struct {
	L1Hits    int64 `json:"l1_hits"`
	L2Hits    int64 `json:"l2_hits"`
	Misses    int64 `json:"misses"`
	L2Dropped int64 `json:"l2_dropped"` // L2 异步写队列满被丢弃的次数
}

type Cache struct {
	db *sql.DB

	mu  sync.RWMutex
	l1  map[string]l1Entry
	l1B int64 // L1 近似占用字节

	l2Sem  chan struct{}  // L2 异步写并发上限
	wg     sync.WaitGroup // 在途 L2 写，Close/Flush 排空
	dropL2 atomic.Int64

	l1Hits atomic.Int64
	l2Hits atomic.Int64
	misses atomic.Int64
}

func New(db *sql.DB) *Cache {
	return &Cache{db: db, l1: make(map[string]l1Entry), l2Sem: make(chan struct{}, 64)}
}

// Get 先查 L1，未命中查 L2（命中则提升回 L1）。
func (c *Cache) Get(ctx context.Context, key string) (string, bool, error) {
	now := time.Now().UnixMilli()

	c.mu.RLock()
	e, ok := c.l1[key]
	c.mu.RUnlock()
	if ok && e.expiresAt > now {
		c.l1Hits.Add(1)
		return e.payload, true, nil
	}

	payload, exp, ok, err := c.getL2(ctx, key, now)
	if err != nil || !ok {
		if !ok {
			c.misses.Add(1)
		}
		return "", false, err
	}
	c.l2Hits.Add(1)
	if len(payload) <= maxL1Payload {
		c.mu.Lock()
		c.l1Set(key, payload, exp, now) // 用 L2 真实过期时间提升
		c.mu.Unlock()
	}
	return payload, true, nil
}

// Set 写两级缓存：L1 同步（纯内存，微秒级）；L2 异步落盘（非阻塞投递，
// 队列满丢弃计数，规范见 docs/CONVENTIONS.md §1.2）。超过大小上限的载荷不缓存。
func (c *Cache) Set(key, payload string, ttl time.Duration) error {
	now := time.Now().UnixMilli()
	exp := now + ttl.Milliseconds()

	if len(payload) <= maxL1Payload {
		c.mu.Lock()
		c.l1Set(key, payload, exp, now)
		c.mu.Unlock()
	}
	if len(payload) <= maxL2Payload {
		select {
		case c.l2Sem <- struct{}{}:
			c.wg.Add(1)
			go func() {
				defer c.wg.Done()
				defer func() { <-c.l2Sem }()
				_ = c.setL2(key, payload, exp) // 异步失败不影响任何响应
			}()
		default:
			c.dropL2.Add(1)
		}
	}
	return nil
}

// Flush 等待在途的 L2 异步写完成（测试与关停前用）。
func (c *Cache) Flush() { c.wg.Wait() }

// Close 关停前排空异步写（与 Flush 等价，语义命名）。
func (c *Cache) Close() { c.wg.Wait() }

// l1Set 写入 L1；容量满时先清过期，仍满则淘汰最旧。调用方需持写锁。
func (c *Cache) l1Set(key, payload string, expiresAt, now int64) {
	if old, ok := c.l1[key]; ok {
		c.l1B -= int64(len(old.payload))
	}
	if len(c.l1) >= maxL1Entries {
		c.l1PruneLocked(now)
	}
	c.l1[key] = l1Entry{payload: payload, expiresAt: expiresAt, addedAt: now}
	c.l1B += int64(len(payload))
}

// l1PruneLocked 清过期；仍满则按 addedAt 淘汰最旧（1024 条线性扫描，低频执行）。
func (c *Cache) l1PruneLocked(now int64) {
	for k, e := range c.l1 {
		if e.expiresAt <= now {
			delete(c.l1, k)
			c.l1B -= int64(len(e.payload))
		}
	}
	for len(c.l1) >= maxL1Entries {
		oldestKey := ""
		var oldest int64 = 1 << 62
		for k, e := range c.l1 {
			if e.addedAt < oldest {
				oldest = e.addedAt
				oldestKey = k
			}
		}
		if oldestKey == "" {
			break
		}
		c.l1B -= int64(len(c.l1[oldestKey].payload))
		delete(c.l1, oldestKey)
	}
}

// L1Size 返回 L1 当前条目数（healthz 指标用）。
func (c *Cache) L1Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.l1)
}

// Snapshot 返回命中统计。
func (c *Cache) Snapshot() Stats {
	return Stats{
		L1Hits:    c.l1Hits.Load(),
		L2Hits:    c.l2Hits.Load(),
		Misses:    c.misses.Load(),
		L2Dropped: c.dropL2.Load(),
	}
}

// ---- L2（SQLite scrape_cache 表）----

func (c *Cache) getL2(ctx context.Context, key string, now int64) (string, int64, bool, error) {
	const q = `SELECT payload, expires_at FROM scrape_cache WHERE cache_key = ? AND expires_at > ?`
	var payload string
	var expiresAt int64
	err := c.db.QueryRowContext(ctx, q, key, now).Scan(&payload, &expiresAt)
	if err == sql.ErrNoRows {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	return payload, expiresAt, true, nil
}

func (c *Cache) setL2(key, payload string, expiresAt int64) error {
	const q = `
INSERT INTO scrape_cache(cache_key, payload, expires_at) VALUES(?, ?, ?)
ON CONFLICT(cache_key) DO UPDATE SET payload = excluded.payload, expires_at = excluded.expires_at`
	// 异步路径：用独立 context，不随请求结束被取消
	if _, err := c.db.ExecContext(context.Background(), q, key, payload, expiresAt); err != nil {
		return err
	}
	if rand.IntN(l2CleanPeriod) == 0 {
		// 清理失败不影响主流程
		_, _ = c.db.ExecContext(context.Background(),
			`DELETE FROM scrape_cache WHERE expires_at <= ?`, time.Now().UnixMilli())
	}
	return nil
}
