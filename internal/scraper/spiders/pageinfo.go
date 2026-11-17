// Package spiders 内置爬虫实现。每个文件一个爬虫。
package spiders

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"

	"api-server/internal/scraper"
)

// PageInfo 示例爬虫：抓取单个网页，返回标题/描述/正文摘要/链接列表。
// 同时演示了本服务的标准姿势：输入校验、SSRF 防护、超时受控、结构化错误。
type PageInfo struct {
	UserAgent string
}

func (p *PageInfo) Name() string { return "pageinfo" }
func (p *PageInfo) Desc() string {
	return `抓取单个网页，返回标题/描述/正文摘要/链接列表。输入: {"url":"https://example.com","max_links":20}`
}

type pageInfoInput struct {
	URL      string `json:"url"`
	MaxLinks int    `json:"max_links"`
}

type pageInfoOutput struct {
	URL         string   `json:"url"`
	HTTPStatus  int      `json:"http_status"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Summary     string   `json:"summary"`
	Links       []string `json:"links"`
	FetchedAt   int64    `json:"fetched_at"`
}

func (p *PageInfo) Run(ctx context.Context, raw json.RawMessage) (any, error) {
	var in pageInfoInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, scraper.ErrBadRequest("请求体必须是 JSON 对象")
	}
	if in.URL == "" {
		return nil, scraper.ErrBadRequest("url 必填")
	}
	if in.MaxLinks <= 0 || in.MaxLinks > 100 {
		in.MaxLinks = 20
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, scraper.ErrBadRequest("url 必须是合法的 http/https 地址")
	}
	if u.User != nil {
		return nil, scraper.ErrBadRequest("url 不允许携带用户信息")
	}
	// SSRF 防护：目标必须解析到公网地址，禁止借道抓内网/元数据接口
	if err := checkPublicHost(ctx, u.Hostname()); err != nil {
		return nil, err
	}

	// colly 不支持外部 context 取消，用剩余预算作为其 HTTP 超时兜底；
	// 外层仍靠 ctx.Done() 保证硬超时。
	reqTimeout := 20 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline) - 500*time.Millisecond; d > time.Second {
			reqTimeout = d
		}
	}

	c := colly.NewCollector(
		colly.AllowedDomains(u.Hostname()), // 跨域重定向直接失败
		colly.UserAgent(p.UserAgent),
		colly.MaxDepth(1),
	)
	c.DisableCookies()
	c.SetRequestTimeout(reqTimeout)

	out := pageInfoOutput{Links: []string{}}
	var fetchErr *scraper.Error
	collected := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(collected) }) }

	c.OnResponse(func(r *colly.Response) {
		defer finish()
		out.HTTPStatus = r.StatusCode
		if r.Request.URL != nil {
			out.URL = r.Request.URL.String()
		} else {
			out.URL = in.URL
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(r.Body)))
		if err != nil {
			fetchErr = scraper.ErrParse("HTML 解析失败: " + err.Error())
			return
		}
		out.Title = strings.TrimSpace(doc.Find("title").First().Text())
		out.Description, _ = doc.Find(`meta[name="description"]`).First().Attr("content")
		out.Description = strings.TrimSpace(out.Description)
		out.Summary = summarize(doc.Find("body").Text(), 500)

		base := *u
		doc.Find("a[href]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			if len(out.Links) >= in.MaxLinks {
				return false
			}
			href, ok := s.Attr("href")
			if !ok || href == "" || strings.HasPrefix(href, "#") {
				return true
			}
			ref, err := url.Parse(href)
			if err != nil {
				return true
			}
			abs := base.ResolveReference(ref)
			if abs.Scheme != "http" && abs.Scheme != "https" {
				return true
			}
			out.Links = append(out.Links, abs.String())
			return true
		})
	})
	c.OnError(func(r *colly.Response, err error) {
		defer finish()
		if r != nil && r.StatusCode > 0 {
			fetchErr = scraper.ErrFetch(fmt.Sprintf("目标站返回 HTTP %d", r.StatusCode))
		} else {
			fetchErr = scraper.ErrFetch("目标站请求失败: " + err.Error())
		}
	})

	go func() { _ = c.Visit(in.URL) }()

	select {
	case <-collected:
	case <-ctx.Done():
		return nil, scraper.ErrTimeout("抓取超时")
	}
	if fetchErr != nil {
		return nil, fetchErr
	}
	out.FetchedAt = time.Now().Unix()
	return out, nil
}

// checkPublicHost 解析目标域名，拒绝回环/内网/链路本地等保留地址。
func checkPublicHost(ctx context.Context, host string) error {
	if host == "" {
		return scraper.ErrBadRequest("url 缺少主机名")
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return scraper.ErrFetch("域名解析失败: " + host)
	}
	for _, ip := range ips {
		if !isPublicIP(ip.IP) {
			return scraper.ErrBadRequest("禁止抓取内网/保留地址: " + ip.IP.String())
		}
	}
	return nil
}

func isPublicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast())
}

// summarize 压缩空白并按 rune 截断为摘要。
func summarize(text string, maxRunes int) string {
	fields := strings.Fields(text)
	s := strings.Join(fields, " ")
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}
