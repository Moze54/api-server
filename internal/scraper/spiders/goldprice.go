// GoldPrice 金价/银价行情：新浪财经公开行情接口。
// 接口要求 Referer: https://finance.sina.com.cn，返回 GBK 编码（2026-11 实测）。
// 行情品种代码示例：hf_XAU 伦敦金现货 / hf_XAG 伦敦银 / hf_GC COMEX黄金 /
// hf_SI COMEX白银 / gds_AUTD 上海金交所黄金延期。
package spiders

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"

	"api-server/internal/scraper"
)

type GoldPrice struct {
	UserAgent string
}

func (g *GoldPrice) Name() string { return "goldprice" }
func (g *GoldPrice) CacheTTL() time.Duration { return 60 * time.Second } // 行情类短缓存
func (g *GoldPrice) Desc() string {
	return `黄金/白银实时行情（新浪财经）。输入: {"codes":["hf_XAU"]} 可选，默认全部常见品种`
}

var goldDefaultCodes = []string{"hf_XAU", "hf_XAG", "hf_GC", "hf_SI", "gds_AUTD"}

var (
	goldCodeRe = regexp.MustCompile(`^[a-zA-Z0-9_]{2,20}$`)
	goldLineRe = regexp.MustCompile(`var hq_str_([a-zA-Z0-9_]+)="([^"]*)"`)
)

type goldInput struct {
	Codes []string `json:"codes"`
}

type goldItem struct {
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Change    float64 `json:"change"`
	ChangePct float64 `json:"change_pct"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	PrevClose float64 `json:"prev_close"`
	Time      string  `json:"time"`
	Date      string  `json:"date"`
}

type goldOutput struct {
	UpdatedAt int64      `json:"updated_at"`
	Items     []goldItem `json:"items"`
}

func (g *GoldPrice) Run(ctx context.Context, raw json.RawMessage) (any, error) {
	var in goldInput
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, scraper.ErrBadRequest("请求体必须是 JSON 对象")
		}
	}
	codes := in.Codes
	if len(codes) == 0 {
		codes = goldDefaultCodes
	}
	if len(codes) > 10 {
		return nil, scraper.ErrBadRequest("codes 最多 10 个品种")
	}
	for _, c := range codes {
		if !goldCodeRe.MatchString(c) {
			return nil, scraper.ErrBadRequest("非法品种代码: " + c)
		}
	}

	body, err := g.fetch(ctx, "http://hq.sinajs.cn/list="+strings.Join(codes, ","))
	if err != nil {
		return nil, scraper.ErrFetch("新浪行情请求失败: " + err.Error())
	}
	// 接口为 GBK 编码
	text, derr := simplifiedchinese.GBK.NewDecoder().Bytes(body)
	if derr != nil {
		text = body
	}

	items := make([]goldItem, 0, len(codes))
	for _, m := range goldLineRe.FindAllStringSubmatch(string(text), -1) {
		item, perr := parseGoldLine(m[1], m[2])
		if perr == nil {
			items = append(items, *item)
		}
	}
	if len(items) == 0 {
		return nil, scraper.ErrParse("行情响应无可解析数据")
	}
	return goldOutput{UpdatedAt: time.Now().Unix(), Items: items}, nil
}

// parseGoldLine 解析单行行情。新浪外盘字段（2026-11 实测）：
// 0现价 2今开 4最高 5最低 6时间 7昨结 8昨收 12日期 13名称。
func parseGoldLine(code, data string) (*goldItem, error) {
	f := strings.Split(data, ",")
	if len(f) < 14 {
		return nil, scraper.ErrParse("行情字段不足: " + code)
	}
	price := num(f[0])
	if price <= 0 {
		return nil, scraper.ErrParse("现价异常: " + code)
	}
	prev := num(f[8])
	if prev <= 0 {
		prev = num(f[7]) // 无昨收时用昨结
	}
	change := round3(price - prev)
	pct := 0.0
	if prev > 0 {
		pct = round3(change / prev * 100)
	}
	return &goldItem{
		Code:      code,
		Name:      strings.TrimSpace(f[13]),
		Price:     round3(price),
		Change:    change,
		ChangePct: pct,
		Open:      round3(num(f[2])),
		High:      round3(num(f[4])),
		Low:       round3(num(f[5])),
		PrevClose: round3(prev),
		Time:      strings.TrimSpace(f[6]),
		Date:      strings.TrimSpace(f[12]),
	}, nil
}

func (g *GoldPrice) fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn/")
	req.Header.Set("User-Agent", g.UserAgent)
	resp, err := weatherHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{code: resp.StatusCode}
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func num(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
