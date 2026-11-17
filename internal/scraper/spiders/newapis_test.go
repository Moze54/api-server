package spiders

import (
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// 从 2026-11-16 实测响应裁剪的样例，固化解析格式。
func TestParseGoldLine(t *testing.T) {
	line := `3866.08,3879.920,3866.08,3866.43,3901.15,3832.36,23:46:00,3879.92,3873.03,0,0,0,2026-11-16,伦敦金（现货黄金）`
	item, err := parseGoldLine("hf_XAU", line)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if item.Price != 3866.08 || item.High != 3901.15 || item.Low != 3832.36 ||
		item.PrevClose != 3873.03 || item.Date != "2026-11-16" {
		t.Fatalf("字段错误: %+v", item)
	}
	if item.Change != -6.95 || item.ChangePct != -0.179 {
		t.Fatalf("涨跌计算错误: change=%v pct=%v", item.Change, item.ChangePct)
	}
	// 无昨收时回落昨结
	line2 := `3865.549,,3869.400,3869.700,3904.300,3835.200,23:46:13,3886.300,,0,2,4,2026-11-16,COMEX黄金`
	item2, err := parseGoldLine("hf_GC", line2)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if item2.PrevClose != 3886.3 {
		t.Fatalf("昨结回落错误: %+v", item2)
	}
}

func TestGoldGBKDecode(t *testing.T) {
	// 接口返回 GBK：模拟 GBK 编码后应能正确解码出中文
	gbk, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(`伦敦金`))
	utf8, err := simplifiedchinese.GBK.NewDecoder().Bytes(gbk)
	if err != nil || string(utf8) != "伦敦金" {
		t.Fatalf("GBK 解码失败: %q %v", utf8, err)
	}
}

func TestParseZhihuHot(t *testing.T) {
	cases := map[string]int64{
		"4930 万热度": 49300000,
		"1.2 亿热度":  120000000,
		"8765热度":   8765,
	}
	for in, want := range cases {
		if got := parseZhihuHot(in); got != want {
			t.Fatalf("%q → %d, 期望 %d", in, got, want)
		}
	}
}

func TestEpicFreeWindow(t *testing.T) {
	el := map[string]any{
		"promotions": map[string]any{
			"promotionalOffers": []any{
				map[string]any{
					"discountSetting":   map[string]any{"discountPercentage": float64(0)},
					"promotionalDates": map[string]any{
						"startDate": "2026-11-13T16:00:00.000Z",
						"endDate":   "2026-11-20T16:00:00.000Z",
					},
				},
			},
		},
	}
	now, _ := time.Parse(time.RFC3339, "2026-11-16T12:00:00Z")
	start, end, free := epicFreeWindow(el, now)
	if !free || start == "" || end == "" {
		t.Fatalf("活动期内应判定为当前免费: %v %v %v", start, end, free)
	}

	later, _ := time.Parse(time.RFC3339, "2026-11-25T12:00:00Z")
	_, _, free2 := epicFreeWindow(el, later)
	if free2 {
		t.Fatal("活动期外不应判定免费")
	}
}
