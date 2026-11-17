// TodayHistory 历史上的今天（百度百科 CMS 接口，60s 项目生产同款数据源）。
// https://baike.baidu.com/cms/home/eventsOnHistory/{MM}.json —— 整月一个文件
// （约 350KB，按 MMDD 分组），命中 TTL 缓存后整月共用一次抓取。
package spiders

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"api-server/internal/scraper"
)

type TodayHistory struct {
	UserAgent string
}

func (t *TodayHistory) Name() string { return "todayhistory" }
func (t *TodayHistory) Desc() string {
	return `历史上的今天（含年份/描述/百科链接）。输入: {"date":"11-17"} 可选（默认今天）`
}

var dateMDRe = regexp.MustCompile(`^(\d{1,2})-(\d{1,2})$`)
var htmlTagRe = regexp.MustCompile(`<[^>]+>`)
var htmlEntityRe = regexp.MustCompile(`&#(\d+);`)

type historyInput struct {
	Date string `json:"date"` // "MM-DD"
}

type historyEvent struct {
	Year   string `json:"year"`
	Title  string `json:"title"`
	Type   string `json:"type,omitempty"`
	Desc   string `json:"desc,omitempty"`
	Link   string `json:"link,omitempty"`
}

type historyOutput struct {
	Date     string         `json:"date"`
	Festival string         `json:"festival,omitempty"`
	Count    int            `json:"count"`
	Events   []historyEvent `json:"events"`
}

func (t *TodayHistory) Run(ctx context.Context, raw json.RawMessage) (any, error) {
	var in historyInput
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, scraper.ErrBadRequest("请求体必须是 JSON 对象")
		}
	}
	month, day := "", ""
	if strings.TrimSpace(in.Date) == "" {
		now := time.Now()
		month, day = pad2Int(int(now.Month())), pad2Int(now.Day())
	} else {
		m := dateMDRe.FindStringSubmatch(strings.TrimSpace(in.Date))
		if m == nil {
			return nil, scraper.ErrBadRequest(`date 格式须为 "MM-DD"，如 "11-17"`)
		}
		mi, e1 := strconv.Atoi(m[1])
		di, e2 := strconv.Atoi(m[2])
		if e1 != nil || e2 != nil || mi < 1 || mi > 12 || di < 1 || di > 31 {
			return nil, scraper.ErrBadRequest(`date 须为合法的 "MM-DD" 日期`)
		}
		month, day = pad2Int(mi), pad2Int(di)
	}

	// 整月文件 + 本地按日过滤
	url := "https://baike.baidu.com/cms/home/eventsOnHistory/" + month + ".json"
	body, err := httpGetJSON2(ctx, url, t.UserAgent, "https://baike.baidu.com/")
	if err != nil {
		return nil, scraper.ErrFetch("历史今天请求失败: " + err.Error())
	}
	var root map[string]map[string][]struct {
		Year     string `json:"year"`
		Title    string `json:"title"`
		Type     string `json:"type"`
		Desc     string `json:"desc"`
		Link     string `json:"link"`
		Festival string `json:"festival"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, scraper.ErrParse("历史今天解析失败: " + err.Error())
	}
	monthData, ok := root[month]
	if !ok {
		return nil, scraper.ErrParse("月份维度缺失: " + month)
	}
	dayKey := month + day
	events, ok := monthData[dayKey]
	if !ok || len(events) == 0 {
		return nil, scraper.ErrParse("该日期无事件数据: " + dayKey)
	}

	out := historyOutput{Date: month + "-" + day, Events: []historyEvent{}}
	for _, e := range events {
		title := stripHTML(e.Title)
		if title == "" {
			continue
		}
		if out.Festival == "" {
			out.Festival = stripHTML(e.Festival)
		}
		out.Events = append(out.Events, historyEvent{
			Year:  e.Year,
			Title: title,
			Type:  e.Type,
			Desc:  stripHTML(e.Desc),
			Link:  e.Link,
		})
	}
	// 按年份升序
	sort.Slice(out.Events, func(i, j int) bool { return out.Events[i].Year < out.Events[j].Year })
	out.Count = len(out.Events)
	return out, nil
}

// stripHTML 去标签 + 解码 &#xxxx; 数字实体。
func stripHTML(s string) string {
	s = htmlTagRe.ReplaceAllString(s, "")
	s = htmlEntityRe.ReplaceAllStringFunc(s, func(m string) string {
		n, _ := strconv.ParseInt(htmlEntityRe.FindStringSubmatch(m)[1], 10, 32)
		return string(rune(n))
	})
	return strings.TrimSpace(s)
}

func pad2Int(n int) string {
	s := strconv.Itoa(n)
	if len(s) == 1 {
		return "0" + s
	}
	return s
}
