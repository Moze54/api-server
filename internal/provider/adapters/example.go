// Package adapters 厂商适配器实现。每个文件一个厂商。
package adapters

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"api-server/internal/provider"
	"api-server/internal/resp"
)

// GitHub 示例厂商适配器：查询仓库公开信息。
// 演示三件事：从环境变量注入密钥、失败重试退避、上游错误码映射。
// 可作为新适配器的模板复制。
type GitHub struct {
	Token string // 可空；从环境变量 VENDOR_GITHUB_TOKEN 读取，用于提升匿名限额
	HTTP  *http.Client
}

func NewGitHub() *GitHub {
	return &GitHub{
		Token: os.Getenv("VENDOR_GITHUB_TOKEN"),
		HTTP:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (g *GitHub) Name() string { return "github" }
func (g *GitHub) Desc() string {
	return `查询 GitHub 仓库公开信息（示例适配器）。输入: {"owner":"golang","repo":"go"}`
}

type githubInput struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
}

type githubOutput struct {
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Stars       int    `json:"stars"`
	Forks       int    `json:"forks"`
	OpenIssues  int    `json:"open_issues"`
	Language    string `json:"language"`
	URL         string `json:"url"`
}

type githubAPIResp struct {
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Stargazers  int    `json:"stargazers_count"`
	Forks       int    `json:"forks_count"`
	OpenIssues  int    `json:"open_issues_count"`
	Language    string `json:"language"`
	HTMLURL     string `json:"html_url"`
	Message     string `json:"message"` // 错误响应里的说明
}

func (g *GitHub) Call(ctx context.Context, raw json.RawMessage) (any, error) {
	var in githubInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, &provider.Error{Code: resp.CodeBadRequest, Message: "请求体必须是 JSON 对象"}
	}
	in.Owner = strings.TrimSpace(in.Owner)
	in.Repo = strings.TrimSpace(in.Repo)
	if in.Owner == "" || in.Repo == "" {
		return nil, &provider.Error{Code: resp.CodeBadRequest, Message: "owner/repo 必填"}
	}

	// 最多 3 次尝试；只有网络错误、5xx、429 才重试，4xx 直接映射返回
	var lastErr error
	delays := []time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond}
	for attempt := 0; attempt < len(delays); attempt++ {
		if d := delays[attempt]; d > 0 {
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return nil, &provider.Error{Code: resp.CodeUpstreamTimeout, Message: "上游调用超时", Cause: ctx.Err()}
			}
		}
		out, status, err := g.fetchOnce(ctx, in)
		if err == nil {
			return out, nil
		}
		if status >= 400 && status < 500 && status != http.StatusTooManyRequests {
			return nil, err // 客户端类错误，重试无意义
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("未知错误")
	}
	return nil, &provider.Error{Code: resp.CodeUpstreamFetch, Message: "上游连续 3 次调用失败", Cause: lastErr}
}

// fetchOnce 返回 (输出, HTTP 状态码, 错误)。错误为 *provider.Error 时已是映射后的业务错误。
func (g *GitHub) fetchOnce(ctx context.Context, in githubInput) (*githubOutput, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://api.github.com/repos/%s/%s", in.Owner, in.Repo), nil)
	if err != nil {
		return nil, 0, &provider.Error{Code: resp.CodeInternal, Message: "构造请求失败", Cause: err}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}

	respHTTP, err := g.HTTP.Do(req)
	if err != nil {
		return nil, 0, &provider.Error{Code: resp.CodeUpstreamFetch, Message: "上游网络错误", Cause: err}
	}
	defer respHTTP.Body.Close()
	status := respHTTP.StatusCode

	body, err := io.ReadAll(io.LimitReader(bufio.NewReader(respHTTP.Body), 1<<20))
	if err != nil {
		return nil, status, &provider.Error{Code: resp.CodeUpstreamFetch, Message: "读取上游响应失败", Cause: err}
	}

	var api githubAPIResp
	if err := json.Unmarshal(body, &api); err != nil {
		return nil, status, &provider.Error{Code: resp.CodeUpstreamParse,
			Message: fmt.Sprintf("上游响应解析失败（HTTP %d）", status), Cause: err}
	}

	if status == http.StatusNotFound {
		return nil, status, &provider.Error{Code: resp.CodeUpstreamMissing,
			Message: "上游资源不存在: " + api.Message}
	}
	if status != http.StatusOK {
		msg := api.Message
		if msg == "" {
			msg = fmt.Sprintf("上游返回 HTTP %d", status)
		}
		return nil, status, &provider.Error{Code: resp.CodeUpstreamFetch, Message: msg}
	}

	return &githubOutput{
		FullName:    api.FullName,
		Description: api.Description,
		Stars:       api.Stargazers,
		Forks:       api.Forks,
		OpenIssues:  api.OpenIssues,
		Language:    api.Language,
		URL:         api.HTMLURL,
	}, status, nil
}
