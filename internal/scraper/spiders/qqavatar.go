// QQAvatar QQ头像直链（腾讯官方公开接口 q1.qlogo.cn，2026-11 实测）。
// 纯模板拼接，零上游请求，微秒级返回。
package spiders

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"api-server/internal/scraper"
)

type QQAvatar struct{}

func (q *QQAvatar) Name() string { return "qqavatar" }
func (q *QQAvatar) Desc() string {
	return `QQ头像直链。输入: {"qq":"10000","size":640}（size 可选 40/100/140/640）`
}

var qqSizes = map[int]bool{40: true, 100: true, 140: true, 640: true}

type qqInput struct {
	QQ   string `json:"qq"`
	Size int    `json:"size"`
}

type qqOutput struct {
	QQ     string `json:"qq"`
	Size   int    `json:"size"`
	Avatar string `json:"avatar"`
}

func (q *QQAvatar) Run(_ context.Context, raw json.RawMessage) (any, error) {
	var in qqInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, scraper.ErrBadRequest("请求体必须是 JSON 对象")
	}
	qq := strings.TrimSpace(in.QQ)
	if qq == "" || len(qq) > 12 {
		return nil, scraper.ErrBadRequest("qq 必填（位数 5-12 的QQ号）")
	}
	for _, r := range qq {
		if r < '0' || r > '9' {
			return nil, scraper.ErrBadRequest("qq 必须是纯数字QQ号")
		}
	}
	size := in.Size
	if size == 0 {
		size = 640
	}
	if !qqSizes[size] {
		return nil, scraper.ErrBadRequest("size 只支持 40/100/140/640")
	}
	return qqOutput{
		QQ:     qq,
		Size:   size,
		Avatar: "https://q1.qlogo.cn/g?b=qq&nk=" + qq + "&s=" + strconv.Itoa(size),
	}, nil
}
