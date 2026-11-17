// EpicFree Epic Games 商城免费游戏（官方公开数据接口，2026-11 实测）。
// 现在免费（折扣 100%）与即将免费分开返回。
package spiders

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"api-server/internal/scraper"
)

type EpicFree struct{}

func (e *EpicFree) Name() string { return "epicfree" }
func (e *EpicFree) Desc() string {
	return `Epic 商城免费游戏（当前免费 + 即将免费）。无参数`
}

const epicURL = "https://store-site-backend-static.ak.epicgames.com/freeGamesPromotions?locale=zh-CN&country=CN&allowCountries=CN"

type epicGame struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Price       string `json:"price"` // 原价（元），免费为 "0"
	StartDate   string `json:"start_date,omitempty"`
	EndDate     string `json:"end_date,omitempty"`
	Image       string `json:"image,omitempty"`
	URL         string `json:"url,omitempty"`
}

type epicOutput struct {
	CurrentFree []epicGame `json:"current_free"`
	Upcoming    []epicGame `json:"upcoming"`
	UpdatedAt   int64      `json:"updated_at"`
}

func (e *EpicFree) Run(ctx context.Context, _ json.RawMessage) (any, error) {
	body, err := httpGetJSON(ctx, epicURL, e.UserAgent())
	if err != nil {
		return nil, scraper.ErrFetch("Epic 接口请求失败: " + err.Error())
	}
	var root struct {
		Data struct {
			Catalog struct {
				SearchStore struct {
					Elements []map[string]any `json:"elements"`
				} `json:"searchStore"`
			} `json:"Catalog"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, scraper.ErrParse("Epic 响应解析失败: " + err.Error())
	}
	elements := root.Data.Catalog.SearchStore.Elements
	if len(elements) == 0 {
		return nil, scraper.ErrParse("Epic 响应无数据")
	}

	now := time.Now()
	out := epicOutput{CurrentFree: []epicGame{}, Upcoming: []epicGame{}, UpdatedAt: now.Unix()}
	for _, el := range elements {
		game := epicGame{
			Title:       str(el, "title"),
			Description: str(el, "description"),
			Price:       epicPrice(el),
			Image:       epicImage(el),
			URL:         epicURL2(el),
		}
		start, end, free := epicFreeWindow(el, now)
		if free {
			game.StartDate = start
			game.EndDate = end
			out.CurrentFree = append(out.CurrentFree, game)
		} else if start != "" {
			game.StartDate = start
			game.EndDate = end
			out.Upcoming = append(out.Upcoming, game)
		}
	}
	return out, nil
}

func (e *EpicFree) UserAgent() string { return "api-server/0.1" }

func epicPrice(el map[string]any) string {
	p, ok := el["price"].(map[string]any)
	if !ok {
		return "0"
	}
	tp, ok := p["totalPrice"].(map[string]any)
	if !ok {
		return "0"
	}
	orig := num(strAny(tp["originalPrice"]))
	return fmt.Sprintf("%.2f", orig/100)
}

// epicFreeWindow 判断当前/即将免费；折扣 0（免费）且现在处于活动期 → 免费。
func epicFreeWindow(el map[string]any, now time.Time) (start, end string, free bool) {
	promos, ok := el["promotions"].(map[string]any)
	if !ok {
		return "", "", false
	}
	offers, _ := promos["promotionalOffers"].([]any)
	for _, o := range offers {
		om, ok := o.(map[string]any)
		if !ok {
			continue
		}
		ds, ok := om["discountSetting"].(map[string]any)
		if !ok {
			continue // 缺折扣设置无法判定，跳过（否则会误判为免费）
		}
		pct, _ := ds["discountPercentage"].(float64)
		dates, _ := om["promotionalDates"].(map[string]any)
		sd := str(dates, "startDate")
		ed := str(dates, "endDate")
		if pct != 0 && pct != 100 {
			continue
		}
		s, _ := time.Parse(time.RFC3339, sd)
		e, _ := time.Parse(time.RFC3339, ed)
		if !now.Before(s) && (ed == "" || now.Before(e)) {
			return sd, ed, true
		}
		if s.After(now) && start == "" {
			start, end = sd, ed
		}
	}
	return start, end, false
}

func epicImage(el map[string]any) string {
	imgs, _ := el["keyImages"].([]any)
	for _, im := range imgs {
		m, ok := im.(map[string]any)
		if !ok {
			continue
		}
		if str(m, "type") == "OfferImageTall" {
			return str(m, "url")
		}
	}
	return ""
}

func epicURL2(el map[string]any) string {
	slug := str(el, "productSlug")
	if slug == "" {
		if ns, ok := el["catalogNs"].(map[string]any); ok {
			if maps, ok := ns["mappings"].([]any); ok && len(maps) > 0 {
				if m, ok := maps[0].(map[string]any); ok {
					slug = str(m, "pageSlug")
				}
			}
		}
	}
	if slug == "" {
		return ""
	}
	return "https://store.epicgames.com/zh-CN/p/" + slug
}

// strAny 取 any 值为字符串（数字原样转）。
func strAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if f, ok := v.(float64); ok {
		return fmt.Sprintf("%v", f)
	}
	return ""
}
