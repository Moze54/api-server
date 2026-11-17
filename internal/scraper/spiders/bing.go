// Bing 必应每日壁纸（官方归档接口 HPImageArchive，2026-11 实测）。
// 最多回溯 8 天（接口限制）。
package spiders

import (
	"context"
	"encoding/json"
	"fmt"

	"api-server/internal/scraper"
)

type Bing struct {
	UserAgent string
}

func (b *Bing) Name() string { return "bing" }
func (b *Bing) Desc() string {
	return `必应每日壁纸（近N天，含版权说明）。输入: {"days":7}（1-8，默认7）`
}

type bingInput struct {
	Days int `json:"days"`
}

type bingImage struct {
	Date      string `json:"date"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Copyright string `json:"copyright"`
}

type bingOutput struct {
	Count  int         `json:"count"`
	Images []bingImage `json:"images"`
}

func (b *Bing) Run(ctx context.Context, raw json.RawMessage) (any, error) {
	var in bingInput
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, scraper.ErrBadRequest("请求体必须是 JSON 对象")
		}
	}
	days := in.Days
	if days <= 0 {
		days = 7
	}
	if days > 8 {
		days = 8
	}
	url := fmt.Sprintf("https://cn.bing.com/HPImageArchive.aspx?format=js&idx=0&n=%d", days)

	body, err := httpGetJSON(ctx, url, b.UserAgent)
	if err != nil {
		return nil, scraper.ErrFetch("必应壁纸接口请求失败: " + err.Error())
	}
	var root struct {
		Images []struct {
			StartDate string `json:"startdate"`
			URL       string `json:"url"`
			Title     string `json:"title"`
			Copyright string `json:"copyright"`
		} `json:"images"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, scraper.ErrParse("必应壁纸解析失败: " + err.Error())
	}
	images := make([]bingImage, 0, len(root.Images))
	for _, im := range root.Images {
		if im.URL == "" {
			continue
		}
		images = append(images, bingImage{
			Date:      formatDate8(im.StartDate),
			Title:     im.Title,
			URL:       "https://cn.bing.com" + im.URL,
			Copyright: im.Copyright,
		})
	}
	if len(images) == 0 {
		return nil, scraper.ErrParse("必应壁纸无数据")
	}
	return bingOutput{Count: len(images), Images: images}, nil
}

// formatDate8 "20261117" → "2026-11-17"
func formatDate8(s string) string {
	if len(s) != 8 {
		return s
	}
	return s[0:4] + "-" + s[4:6] + "-" + s[6:8]
}
