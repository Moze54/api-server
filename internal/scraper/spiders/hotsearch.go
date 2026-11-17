// HotSearch 多平台热搜榜。数据源 2026-11 实测：
//   - douyin   https://www.iesdouyin.com/web/api/v2/hotsearch/billboard/word/ （需移动端 UA）
//   - zhihu    https://api.zhihu.com/topstory/hot-list
//   - bilibili https://api.bilibili.com/x/web-interface/search/square
// 微博（网页/移动端）当前均有 cookie 反爬，攻坚中暂不提供。
package spiders

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"api-server/internal/scraper"
)

type HotSearch struct {
	UserAgent string
}

func (h *HotSearch) Name() string { return "hotsearch" }
func (h *HotSearch) CacheTTL() time.Duration { return 60 * time.Second } // 榜单类短缓存
func (h *HotSearch) Desc() string {
	return `热搜榜：抖音/知乎/B站。输入: {"platform":"douyin","limit":20}`
}

var hotPlatforms = []string{"douyin", "zhihu", "bilibili"}

const (
	douyinHotURL   = "https://www.iesdouyin.com/web/api/v2/hotsearch/billboard/word/"
	zhihuHotURL    = "https://api.zhihu.com/topstory/hot-list?limit=50"
	bilibiliHotURL = "https://api.bilibili.com/x/web-interface/search/square?limit=50"
	mobileUA       = "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148"
)

type hotInput struct {
	Platform string `json:"platform"`
	Limit    int    `json:"limit"`
}

type hotItem struct {
	Rank  int    `json:"rank"`
	Title string `json:"title"`
	Hot   int64  `json:"hot,omitempty"`
	URL   string `json:"url,omitempty"`
}

type hotOutput struct {
	Platform  string    `json:"platform"`
	UpdatedAt int64     `json:"updated_at"`
	Items     []hotItem `json:"items"`
}

func (h *HotSearch) Run(ctx context.Context, raw json.RawMessage) (any, error) {
	var in hotInput
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, scraper.ErrBadRequest("请求体必须是 JSON 对象")
		}
	}
	platform := strings.ToLower(strings.TrimSpace(in.Platform))
	if platform == "" {
		platform = "douyin"
	}
	valid := false
	for _, p := range hotPlatforms {
		if p == platform {
			valid = true
			break
		}
	}
	if !valid {
		return nil, scraper.ErrBadRequest("platform 取值须为 douyin / zhihu / bilibili")
	}
	limit := in.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	var items []hotItem
	var err error
	switch platform {
	case "douyin":
		items, err = h.douyin(ctx, limit)
	case "zhihu":
		items, err = h.zhihu(ctx, limit)
	case "bilibili":
		items, err = h.bilibili(ctx, limit)
	}
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, scraper.ErrParse("热搜响应无可解析数据")
	}
	return hotOutput{Platform: platform, UpdatedAt: time.Now().Unix(), Items: items}, nil
}

func (h *HotSearch) douyin(ctx context.Context, limit int) ([]hotItem, error) {
	body, err := httpGetJSON(ctx, douyinHotURL, mobileUA)
	if err != nil {
		return nil, scraper.ErrFetch("抖音热搜请求失败: " + err.Error())
	}
	var root struct {
		WordList []struct {
			Word     string  `json:"word"`
			HotValue float64 `json:"hot_value"`
		} `json:"word_list"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, scraper.ErrParse("抖音热搜解析失败: " + err.Error())
	}
	items := make([]hotItem, 0, limit)
	for i, w := range root.WordList {
		if i >= limit {
			break
		}
		items = append(items, hotItem{
			Rank:  i + 1,
			Title: w.Word,
			Hot:   int64(w.HotValue),
			URL:   "https://www.douyin.com/search/" + url.QueryEscape(w.Word),
		})
	}
	return items, nil
}

var zhihuHotRe = regexp.MustCompile(`([\d\.]+)\s*(万|亿)?`)

func (h *HotSearch) zhihu(ctx context.Context, limit int) ([]hotItem, error) {
	body, err := httpGetJSON(ctx, zhihuHotURL, h.UserAgent)
	if err != nil {
		return nil, scraper.ErrFetch("知乎热榜请求失败: " + err.Error())
	}
	var root struct {
		Data []struct {
			Target struct {
				Title string `json:"title"`
				ID    int64  `json:"id"`
			} `json:"target"`
			DetailText string `json:"detail_text"` // 如 "4930 万热度"
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, scraper.ErrParse("知乎热榜解析失败: " + err.Error())
	}
	items := make([]hotItem, 0, limit)
	for i, d := range root.Data {
		if i >= limit {
			break
		}
		items = append(items, hotItem{
			Rank:  i + 1,
			Title: d.Target.Title,
			Hot:   parseZhihuHot(d.DetailText),
			URL:   "https://www.zhihu.com/question/" + strconv.FormatInt(d.Target.ID, 10),
		})
	}
	return items, nil
}

func parseZhihuHot(s string) int64 {
	m := zhihuHotRe.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	switch m[2] {
	case "万":
		v *= 10000
	case "亿":
		v *= 100000000
	}
	return int64(v)
}

func (h *HotSearch) bilibili(ctx context.Context, limit int) ([]hotItem, error) {
	body, err := httpGetJSON(ctx, bilibiliHotURL, h.UserAgent)
	if err != nil {
		return nil, scraper.ErrFetch("B站热搜请求失败: " + err.Error())
	}
	var root struct {
		Data struct {
			Trending struct {
				List []struct {
					Keyword   string `json:"keyword"`
					ShowName  string `json:"show_name"`
					HeatScore int64  `json:"heat_score"`
				} `json:"list"`
			} `json:"trending"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, scraper.ErrParse("B站热搜解析失败: " + err.Error())
	}
	items := make([]hotItem, 0, limit)
	for i, w := range root.Data.Trending.List {
		if i >= limit {
			break
		}
		title := w.ShowName
		if title == "" {
			title = w.Keyword
		}
		items = append(items, hotItem{
			Rank:  i + 1,
			Title: title,
			Hot:   w.HeatScore,
			URL:   "https://search.bilibili.com/all?keyword=" + url.QueryEscape(w.Keyword),
		})
	}
	return items, nil
}
