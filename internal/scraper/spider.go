// Package scraper 定义爬虫（Spider）接口、注册表与业务错误。
// 新增一个爬虫 = 在 spiders/ 下加一个实现 Spider 的文件并在 main 注册。
package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"api-server/internal/resp"
)

// Error 业务错误：Handler 据此映射统一响应。
type Error struct {
	Code    int    // resp 业务码
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

func ErrBadRequest(msg string) *Error { return &Error{Code: resp.CodeBadRequest, Message: msg} }
func ErrTimeout(msg string) *Error    { return &Error{Code: resp.CodeUpstreamTimeout, Message: msg} }
func ErrFetch(msg string) *Error      { return &Error{Code: resp.CodeUpstreamFetch, Message: msg} }
func ErrParse(msg string) *Error      { return &Error{Code: resp.CodeUpstreamParse, Message: msg} }

// Spider 单个爬虫能力的接口。
type Spider interface {
	Name() string
	Desc() string
	// Run 执行抓取与解析。input 是请求的原始 JSON body；
	// 返回值会被 JSON 序列化后作为 data 字段返回给调用方（并写入缓存）。
	Run(ctx context.Context, input json.RawMessage) (any, error)
}

// Registry 爬虫注册表。
type Registry struct {
	mu sync.RWMutex
	m  map[string]Spider
}

func NewRegistry() *Registry {
	return &Registry{m: make(map[string]Spider)}
}

// Register 注册爬虫，重名直接报错（在 main 启动时暴露配置问题）。
func (r *Registry) Register(s Spider) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.m[s.Name()]; dup {
		return fmt.Errorf("爬虫 %q 重复注册", s.Name())
	}
	r.m[s.Name()] = s
	return nil
}

func (r *Registry) Get(name string) (Spider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.m[name]
	return s, ok
}

// List 按名字排序返回全部已注册爬虫。
func (r *Registry) List() []Spider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.m))
	for n := range r.m {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Spider, 0, len(names))
	for _, n := range names {
		out = append(out, r.m[n])
	}
	return out
}

// NoCache 缓存豁免标记：随机类爬虫（一言/随机图）实现它，声明结果不做缓存——
// 缓存会消灭随机性。Handler 据此跳过 Get/Set。
type NoCache interface {
	NoCache() bool
}

// CacheTTL 可选接口：爬虫声明专属缓存 TTL，覆盖全局默认值。
// 行情类（金价/热搜）数据时效性强，应返回短 TTL。
type CacheTTL interface {
	CacheTTL() time.Duration
}

// ErrBusy 并发池槽位满，由 Handler 映射为 42902。
var ErrBusy = errors.New("scraper pool busy")
