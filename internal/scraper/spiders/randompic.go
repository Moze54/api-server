// RandomPic 随机图片（返回可直连的图片地址）。数据源 2026-11 实测：
//   - scenery 风景: picsum.photos（v2 列表随机取图，可指定尺寸）
//   - acg     二次元: api.paugram.com（302 → 图床 CDN，服务端解析出最终地址）
package spiders

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"api-server/internal/scraper"
)

type RandomPic struct {
	UserAgent string
}

func (r *RandomPic) Name() string { return "randompic" }
func (r *RandomPic) NoCache() bool { return true } // 随机语义，缓存会消灭随机性
func (r *RandomPic) Desc() string {
	return `随机图片。输入: {"type":"scenery|acg","width":1920,"height":1080}（尺寸仅 scenery 生效）`
}

type picInput struct {
	Type   string `json:"type"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type picOutput struct {
	Type   string `json:"type"`
	URL    string `json:"url"`
	Source string `json:"source"`
}

func (r *RandomPic) Run(ctx context.Context, raw json.RawMessage) (any, error) {
	var in picInput
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, scraper.ErrBadRequest("请求体必须是 JSON 对象")
		}
	}
	typ := strings.ToLower(strings.TrimSpace(in.Type))
	if typ == "" {
		typ = "scenery"
	}

	switch typ {
	case "scenery":
		// picsum v2 列表：随机页取一图（站内总量约 1000 图，页码限 1-30 保证非空），按请求尺寸重建直链
		page := rand.IntN(30) + 1
		body, err := httpGetJSON(ctx, "https://picsum.photos/v2/list?page="+strconv.Itoa(page)+"&limit=25", r.UserAgent)
		if err != nil {
			return nil, scraper.ErrFetch("图片列表请求失败: " + err.Error())
		}
		var list []struct {
			ID       string `json:"id"`
			Author   string `json:"author"`
			Download string `json:"download_url"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, scraper.ErrParse("图片列表解析失败")
		}
		if len(list) == 0 { // 兜底回第一页
			if body, err = httpGetJSON(ctx, "https://picsum.photos/v2/list?page=1&limit=25", r.UserAgent); err != nil {
				return nil, scraper.ErrFetch("图片列表请求失败: " + err.Error())
			}
			if err := json.Unmarshal(body, &list); err != nil || len(list) == 0 {
				return nil, scraper.ErrParse("图片列表解析失败")
			}
		}
		pick := list[rand.IntN(len(list))]
		w, hgt := clampPicSize(in.Width, in.Height)
		return picOutput{
			Type:   typ,
			URL:    "https://picsum.photos/id/" + pick.ID + "/" + strconv.Itoa(w) + "/" + strconv.Itoa(hgt),
			Source: pick.Author,
		}, nil

	case "acg":
		// paugram 302 → 图床 CDN；只取 Location 不下载图片本体
		final, err := resolveRedirect(ctx, "https://api.paugram.com/wallpaper/", r.UserAgent)
		if err != nil {
			return nil, scraper.ErrFetch("二次元图源请求失败: " + err.Error())
		}
		return picOutput{Type: typ, URL: final, Source: "paugram"}, nil

	default:
		return nil, scraper.ErrBadRequest("type 取值须为 scenery / acg")
	}
}

func clampPicSize(w, hgt int) (int, int) {
	if w <= 0 || w > 5000 {
		w = 1920
	}
	if hgt <= 0 || hgt > 5000 {
		hgt = 1080
	}
	return w, hgt
}

// resolveRedirect 请求一次并捕获 302 的目标地址（不跟随、不下载图片）。
// 使用独立 Client 但共享底层连接池（weatherHTTPClient.Transport）。
func resolveRedirect(ctx context.Context, target, ua string) (string, error) {
	cl := &http.Client{
		Transport: weatherHTTPClient.Transport,
		Timeout:   15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", ua)
	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode >= 300 && resp.StatusCode < 400 && loc != "" {
		return loc, nil
	}
	if resp.StatusCode == http.StatusOK {
		// 直接返回了图片本体，那就用源地址本身（调用方嵌入即可获得随机图）
		return target, nil
	}
	return "", &httpStatusError{code: resp.StatusCode}
}
