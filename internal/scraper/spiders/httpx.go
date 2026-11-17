// 包内共享的 HTTP 助手：全部走共享 keep-alive 客户端，统一限长与状态码检查。
package spiders

import (
	"context"
	"io"
	"net/http"
	"time"
)

// httpGetJSON 简单 GET（无 Referer 要求的公开 JSON 接口用）。
func httpGetJSON(ctx context.Context, url, ua string) ([]byte, error) {
	return httpGetJSON2(ctx, url, ua, "")
}

// httpGetJSON2 带可选 Referer 的 GET（部分源校验 Referer）。
// 瞬态错误（网络抖动/5xx）自动重试一次，退避 300ms，受 ctx 约束（规范 §1.3）。
func httpGetJSON2(ctx context.Context, url, ua, referer string) ([]byte, error) {
	body, err := httpGetOnce(ctx, url, ua, referer)
	if err == nil || !isTransient(err) {
		return body, err
	}
	select {
	case <-time.After(300 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return httpGetOnce(ctx, url, ua, referer)
}

func httpGetOnce(ctx context.Context, url, ua, referer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := weatherHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{code: resp.StatusCode}
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}

// httpGetHTML GET HTML 页面（爬虫解析用）。
func httpGetHTML(ctx context.Context, url, ua string) ([]byte, error) {
	return httpGetJSON(ctx, url, ua)
}
