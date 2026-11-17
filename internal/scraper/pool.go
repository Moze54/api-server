// 资源闸门：全局并发信号量 + 单任务硬超时。
// 爬虫任务耗时 2~30 秒不定，不设闸门的话几个并发请求就能把
// 小服务器的 CPU/内存打满，连带影响同机其他项目。
package scraper

import (
	"context"
	"sync/atomic"
	"time"
)

type Pool struct {
	sem      chan struct{}
	timeout  time.Duration // 单任务硬超时
	queue    time.Duration // 等槽位的最长时间，超过返回 ErrBusy
	inFlight atomic.Int64  // 正在执行的任务数（healthz 指标）
}

func NewPool(maxConcurrency, timeoutSeconds, queueSeconds int) *Pool {
	return &Pool{
		sem:     make(chan struct{}, maxConcurrency),
		timeout: time.Duration(timeoutSeconds) * time.Second,
		queue:   time.Duration(queueSeconds) * time.Second,
	}
}

// Run 获取空闲槽位后，在带超时的 context 中执行 fn。
func (p *Pool) Run(ctx context.Context, fn func(context.Context) error) error {
	select {
	case p.sem <- struct{}{}:
	case <-time.After(p.queue):
		return ErrBusy
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-p.sem; p.inFlight.Add(-1) }()
	p.inFlight.Add(1)

	runCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	return fn(runCtx)
}

// InFlight 当前正在执行的任务数。
func (p *Pool) InFlight() int64 { return p.inFlight.Load() }
