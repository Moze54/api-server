// Package provider 定义厂商 API 适配器（Vendor）接口与注册表。
// 新增一个厂商 = 在 adapters/ 下加一个实现 Vendor 的文件并在 main 注册。
// 适配器的标准职责：密钥注入、重试退避、上游错误码到本服务业务码的映射。
//
// 注意：目录名不能用 vendor（Go 保留目录名，会被当作依赖 vendor 树）。
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Error 业务错误：Handler 据此映射统一响应。
type Error struct {
	Code    int
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

// Vendor 单个厂商适配器接口。
type Vendor interface {
	Name() string
	Desc() string
	// Call 执行上游调用。input 是请求的原始 JSON body；
	// 返回值会被 JSON 序列化后作为 data 字段返回给调用方。
	Call(ctx context.Context, input json.RawMessage) (any, error)
}

type Registry struct {
	mu sync.RWMutex
	m  map[string]Vendor
}

func NewRegistry() *Registry {
	return &Registry{m: make(map[string]Vendor)}
}

func (r *Registry) Register(v Vendor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.m[v.Name()]; dup {
		return fmt.Errorf("厂商 %q 重复注册", v.Name())
	}
	r.m[v.Name()] = v
	return nil
}

func (r *Registry) Get(name string) (Vendor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.m[name]
	return v, ok
}

// List 按名字排序返回全部已注册厂商。
func (r *Registry) List() []Vendor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.m))
	for n := range r.m {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Vendor, 0, len(names))
	for _, n := range names {
		out = append(out, r.m[n])
	}
	return out
}
