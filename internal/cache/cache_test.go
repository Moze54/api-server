package cache

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"api-server/internal/store"
)

func TestCacheTTL(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开 store: %v", err)
	}
	defer st.Close()
	c := New(st.DB)
	defer c.Close()
	ctx := context.Background()

	if err := c.Set("k1", `{"a":1}`, 100*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	c.Flush() // 等 L2 异步落盘完成，保证后续断言确定性
	payload, ok, err := c.Get(ctx, "k1")
	if err != nil || !ok || payload != `{"a":1}` {
		t.Fatalf("TTL 内应命中, ok=%v payload=%q err=%v", ok, payload, err)
	}

	time.Sleep(400 * time.Millisecond)
	if _, ok, _ := c.Get(ctx, "k1"); ok {
		t.Fatal("过期后不应命中")
	}

	// 覆盖写
	if err := c.Set("k1", `{"b":2}`, time.Minute); err != nil {
		t.Fatalf("Set 覆盖: %v", err)
	}
	c.Flush()
	payload, ok, _ = c.Get(ctx, "k1")
	if !ok || payload != `{"b":2}` {
		t.Fatalf("覆盖写失败, ok=%v payload=%q", ok, payload)
	}
}

func TestCacheL1(t *testing.T) {
	oldCap := maxL1Entries
	maxL1Entries = 4
	defer func() { maxL1Entries = oldCap }()

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开 store: %v", err)
	}
	defer st.Close()
	c := New(st.DB)
	defer c.Close()
	ctx := context.Background()

	for i := 0; i < 6; i++ {
		if err := c.Set(fmt.Sprintf("k%d", i), fmt.Sprintf("v%d", i), time.Minute); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	c.Flush() // 等 L2 落盘，被逐出的条目可从 L2 兜底读回
	if c.L1Size() > maxL1Entries {
		t.Fatalf("L1 超过容量上限: %d", c.L1Size())
	}
	// 全部 key 仍应可读（被逐出后 L2 兜底）
	for i := 0; i < 6; i++ {
		key := fmt.Sprintf("k%d", i)
		payload, ok, err := c.Get(ctx, key)
		if err != nil || !ok || payload != fmt.Sprintf("v%d", i) {
			t.Fatalf("容量淘汰后 %s 读取失败, ok=%v payload=%q err=%v", key, ok, payload, err)
		}
	}
	if s := c.Snapshot(); s.L1Hits == 0 || s.L2Hits == 0 {
		t.Fatalf("应同时出现 L1 与 L2 命中: %+v", s)
	}
}

func TestCacheSkipOversize(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开 store: %v", err)
	}
	defer st.Close()
	c := New(st.DB)
	defer c.Close()
	ctx := context.Background()

	big := strings.Repeat("x", maxL1Payload+1)
	if err := c.Set("big", big, time.Minute); err != nil {
		t.Fatalf("Set 大载荷不应报错: %v", err)
	}
	if c.L1Size() != 0 {
		t.Fatalf("超限载荷不应进 L1, size=%d", c.L1Size())
	}
	// 超过 L2 上限的完全不缓存
	huge := strings.Repeat("x", maxL2Payload+1)
	if err := c.Set("huge", huge, time.Minute); err != nil {
		t.Fatalf("Set 超限载荷不应报错: %v", err)
	}
	c.Flush()
	// big 超了 L1 但在 L2 上限内 → 可从 L2 读回
	if _, ok, _ := c.Get(ctx, "big"); !ok {
		t.Fatal("仅超 L1 的载荷应从 L2 读回")
	}
	if _, ok, _ := c.Get(ctx, "huge"); ok {
		t.Fatal("超 L2 上限的载荷不应可读")
	}
}
