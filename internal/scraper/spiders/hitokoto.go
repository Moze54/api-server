// Hitokoto 一言（官方 v1.hitokoto.cn，2026-11 实测）。多分类参数。
package spiders

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"api-server/internal/scraper"
)

type Hitokoto struct {
	UserAgent string
}

func (h *Hitokoto) Name() string { return "hitokoto" }
func (h *Hitokoto) NoCache() bool { return true } // 随机语义，缓存会消灭随机性
func (h *Hitokoto) Desc() string {
	return `一言（多分类随机句子）。输入: {"c":["a","i"],"type":"动画"} 可选；分类: 动画a 漫画b 文学c 原创d 网络e 其他f 影视g 网易云h 诗词i 哲学j`
}

var hitokotoCategories = map[string]string{
	"a": "动画", "b": "漫画", "c": "文学", "d": "原创", "e": "网络",
	"f": "其他", "g": "影视", "h": "网易云", "i": "诗词", "j": "哲学",
}

type hitokotoInput struct {
	C    []string `json:"c"`  // 分类代码，最多 5 个
	Type string   `json:"type"` // 分类中文名（与 c 二选一，中文优先）
}

type hitokotoOutput struct {
	Sentence string `json:"hitokoto"`
	From     string `json:"from,omitempty"`
	FromWho  string `json:"from_who,omitempty"`
	Creator  string `json:"creator,omitempty"`
	Category string `json:"category"`
}

func (h *Hitokoto) Run(ctx context.Context, raw json.RawMessage) (any, error) {
	var in hitokotoInput
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, scraper.ErrBadRequest("请求体必须是 JSON 对象")
		}
	}

	// 分类解析：中文优先映射回代码；代码直接用
	codes := in.C
	if t := strings.TrimSpace(in.Type); t != "" {
		matched := ""
		for code, name := range hitokotoCategories {
			if strings.Contains(name, t) || name == t {
				matched = code
				break
			}
		}
		if matched == "" {
			return nil, scraper.ErrBadRequest("未知分类 \"" + t + "\"，可选：动画/漫画/文学/原创/网络/其他/影视/网易云/诗词/哲学")
		}
		codes = []string{matched}
	}
	if len(codes) > 5 {
		return nil, scraper.ErrBadRequest("分类最多 5 个")
	}
	q := url.Values{}
	q.Set("encode", "json")
	for _, c := range codes {
		c = strings.ToLower(strings.TrimSpace(c))
		if _, ok := hitokotoCategories[c]; !ok {
			return nil, scraper.ErrBadRequest("未知分类代码 \"" + c + "\"，可用 a-j（动画/漫画/文学/原创/网络/其他/影视/网易云/诗词/哲学）")
		}
		q.Add("c", c)
	}

	body, err := httpGetJSON(ctx, "https://v1.hitokoto.cn/?"+q.Encode(), h.UserAgent)
	if err != nil {
		return nil, scraper.ErrFetch("一言请求失败: " + err.Error())
	}
	var root struct {
		Hitokoto string `json:"hitokoto"`
		From     string `json:"from"`
		FromWho  string `json:"from_who"`
		Creator  string `json:"creator"`
		Type     string `json:"type"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, scraper.ErrParse("一言解析失败: " + err.Error())
	}
	if root.Hitokoto == "" {
		return nil, scraper.ErrParse("一言无数据")
	}
	category := hitokotoCategories[root.Type]
	if category == "" {
		category = root.Type
	}
	return hitokotoOutput{
		Sentence: root.Hitokoto,
		From:     root.From,
		FromWho:  root.FromWho,
		Creator:  root.Creator,
		Category: category,
	}, nil
}
