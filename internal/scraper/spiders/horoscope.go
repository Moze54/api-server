// Horoscope 星座今日运势（星座屋 xzw.com，2026-11 实测）。
// 页面结构：dl>dt img[title] 星座名 / dd li label+值（星级=em宽度÷16，0-5）/
// 百分比与幸运字段为 li 文本 / div.c_cont p 内 strong.p1+span 为运势概述。
package spiders

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"api-server/internal/scraper"
)

type Horoscope struct {
	UserAgent string
}

func (h *Horoscope) Name() string { return "horoscope" }
func (h *Horoscope) Desc() string {
	return `星座今日运势（评分/幸运颜色/数字/速配/概述）。输入: {"astro":"aries"}`
}

var astroAliases = map[string]string{
	"aries": "aries", "白羊座": "aries", "taurus": "taurus", "金牛座": "taurus",
	"gemini": "gemini", "双子座": "gemini", "cancer": "cancer", "巨蟹座": "cancer",
	"leo": "leo", "狮子座": "leo", "virgo": "virgo", "处女座": "virgo",
	"libra": "libra", "天秤座": "libra", "scorpio": "scorpio", "天蝎座": "scorpio",
	"sagittarius": "sagittarius", "射手座": "sagittarius", "capricorn": "capricorn", "摩羯座": "capricorn",
	"aquarius": "aquarius", "水瓶座": "aquarius", "pisces": "pisces", "双鱼座": "pisces",
}

var starWidthRe = regexp.MustCompile(`width:\s*(\d+)px`)

type horoscopeInput struct {
	Astro string `json:"astro"`
}

type horoscopeOutput struct {
	Astro    string         `json:"astro"`
	Name     string         `json:"name"`
	Date     string         `json:"date,omitempty"`
	Fortune  map[string]int    `json:"fortune"`           // 星级评分 0-5
	Index    map[string]string `json:"index"`             // 健康指数/商谈指数等百分比
	Lucky    map[string]string `json:"lucky,omitempty"`   // 幸运颜色/数字/速配/短评
	Summary  map[string]string `json:"summary,omitempty"` // 运势概述段落
}

func (h *Horoscope) Run(ctx context.Context, raw json.RawMessage) (any, error) {
	var in horoscopeInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, scraper.ErrBadRequest("请求体必须是 JSON 对象")
	}
	code := astroAliases[strings.ToLower(strings.TrimSpace(in.Astro))]
	if code == "" {
		return nil, scraper.ErrBadRequest("astro 必填（如 aries 或 白羊座；可选 12 星座）")
	}

	body, err := httpGetHTML(ctx, "https://www.xzw.com/fortune/"+code+"/", h.UserAgent)
	if err != nil {
		return nil, scraper.ErrFetch("星座运势请求失败: " + err.Error())
	}
	out, perr := parseHoroscope(body, code)
	if perr != nil {
		return nil, perr
	}
	return out, nil
}

func parseHoroscope(body []byte, code string) (*horoscopeOutput, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, scraper.ErrParse("页面解析失败: " + err.Error())
	}

	// 定位运势 dl：dt 里有带 title 的星座图
	var dl *goquery.Selection
	doc.Find("dl").EachWithBreak(func(i int, s *goquery.Selection) bool {
		if t, ok := s.Find("dt img").First().Attr("title"); ok && strings.Contains(t, "座") {
			dl = s
			return false
		}
		return true
	})
	if dl == nil {
		return nil, scraper.ErrParse("未找到运势数据块")
	}

	out := &horoscopeOutput{
		Astro:   code,
		Name:    dl.Find("dt img").First().AttrOr("title", code),
		Date:    strings.TrimSpace(dl.Find("dd h4 small").First().Text()),
		Fortune: map[string]int{},
		Index:   map[string]string{},
		Lucky:   map[string]string{},
		Summary: map[string]string{},
	}

	dl.Find("dd ul li").Each(func(_ int, li *goquery.Selection) {
		label := strings.TrimRight(strings.TrimSpace(li.Find("label").First().Text()), "：")
		if label == "" {
			return
		}
		// 星级：em 宽度 16px/星
		if em := li.Find("em").First(); em.Length() > 0 {
			if m := starWidthRe.FindStringSubmatch(em.AttrOr("style", "")); len(m) > 1 {
				px, _ := strconv.Atoi(m[1])
				out.Fortune[label] = px / 16
				return
			}
		}
		value := strings.TrimSpace(strings.TrimPrefix(li.Text(), li.Find("label").First().Text()))
		value = strings.TrimSuffix(value, "：")
		switch {
		case strings.HasPrefix(label, "幸运") || label == "速配星座" || label == "短评":
			out.Lucky[label] = value
		case strings.HasSuffix(label, "指数"):
			out.Index[label] = strings.TrimSuffix(value, "%")
		default:
			out.Index[label] = value
		}
	})

	// 概述段：div.c_cont > p > strong.p1 + span
	doc.Find("div.c_cont p").Each(func(_ int, p *goquery.Selection) {
		title := strings.TrimSpace(p.Find("strong.p1").First().Text())
		content := strings.TrimSpace(p.Find("span").First().Text())
		if title != "" && content != "" {
			out.Summary[title] = content
		}
	})
	if len(out.Fortune) == 0 {
		return nil, scraper.ErrParse("未解析到运势评分")
	}
	return out, nil
}
