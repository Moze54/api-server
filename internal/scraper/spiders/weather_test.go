package spiders

import (
	"context"
	"encoding/json"
	"testing"
)

// 从真实响应裁剪的样例（2026-11-16 实测），固化解析逻辑防止静默劣化。
const fixtureIndex = `var cityDZ ={"weatherinfo":{"city":"北京"}};` +
	`var alarmDZ ={"w":[]};` +
	`var dataSK ={"nameen":"beijing","cityname":"北京","city":"101010100","temp":"6.8","WD":"南风","WS":"1级","SD":"52%","qy":"1022","njd":"14km","time":"22:20","rain":"0","rain24h":"0","aqi":"42","weather":"多云","date":"11月16日(星期一)"};` +
	`var dataZS ={"zs":{"ct_name":"穿衣指数","ct_hint":"寒冷","ct_des_s":"建议着厚羽绒服等隆冬服装。","gm_name":"感冒指数","gm_hint":"极易发","gm_des_s":"极易感冒。"},"cn":"北京"};` +
	`var fc ={"f":[{"fa":"02","fb":"02","fc":"8","fd":"1","fe":"东风","ff":"北风","fg":"<3级","fh":"<3级","fi":"11/16","fj":"今天"},{"fa":"07","fb":"01","fc":"8","fd":"1","fe":"西风","ff":"西北风","fg":"<3级","fh":"3-4级","fi":"11/17","fj":"星期二"}]};`

const fixtureHTML = `<ul class="t clearfix">
<li class="sky skyid lv1 night"><h1>16日（今天）</h1><p title="阴" class="wea">阴</p>
<p class="tem"><i>1℃</i></p>
<p class="win"><em><span title="北风" class="N"></span></em><i><3级</i></p></li>
<li class="sky skyid lv2"><h1>17日（明天）</h1><p title="小雨转多云" class="wea">小雨转多云</p>
<p class="tem"><span>8℃</span>/<i>1℃</i></p>
<p class="win"><em><span title="西风" class="W"></span><span title="西北风" class="NW"></span></em><i>&lt;3级转3-4级</i></p></li>
</ul>`

func TestExtractVars(t *testing.T) {
	sk := extractJSONObject(dataSKRe, fixtureIndex)
	if sk == nil || str(sk, "temp") != "6.8" || str(sk, "cityname") != "北京" {
		t.Fatalf("dataSK 提取失败: %v", sk)
	}
	zs := extractJSONObject(dataZSRe, fixtureIndex)
	if zs == nil {
		t.Fatal("dataZS 提取失败")
	}
	idx := parseLifeIndexes(zs)
	if len(idx) != 2 || idx[0].Name != "穿衣指数" || idx[0].Hint != "寒冷" ||
		idx[1].Name != "感冒指数" || idx[1].Hint != "极易发" {
		t.Fatalf("生活指数解析错误(应按 base 键排序): %+v", idx)
	}
	fc := extractJSONObject(fcRe, fixtureIndex)
	if fc == nil {
		t.Fatal("fc 提取失败")
	}
	days := parseFC(fc)
	if len(days) != 2 {
		t.Fatalf("fc 兜底应解析出 2 天: %+v", days)
	}
	if days[1].Weather != "小雨转多云" || days[1].High != "8℃" || days[1].Day != "11/17 星期二" {
		t.Fatalf("fc 第二天解析错误: %+v", days[1])
	}
}

func TestParseForecastHTML(t *testing.T) {
	days, err := parseForecastHTML([]byte(fixtureHTML))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(days) != 2 {
		t.Fatalf("应解析出 2 天, got %d", len(days))
	}
	today := days[0]
	if today.Day != "16日（今天）" || today.Weather != "阴" || today.High != "" || today.Low != "1℃" ||
		today.Wind != "北风" || today.WindLevel != "<3级" {
		t.Fatalf("今天(仅夜间)解析错误: %+v", today)
	}
	tomorrow := days[1]
	if tomorrow.Weather != "小雨转多云" || tomorrow.High != "8℃" || tomorrow.Low != "1℃" ||
		tomorrow.Wind != "西风转西北风" || tomorrow.WindLevel != "<3级转3-4级" {
		t.Fatalf("明天解析错误: %+v", tomorrow)
	}
}

func TestWeatherInputValidation(t *testing.T) {
	w := &Weather{UserAgent: "test"}
	ctx := context.Background()

	if _, err := w.Run(ctx, json.RawMessage(`{}`)); err == nil {
		t.Fatal("空输入应报错")
	}
	_, err := w.Run(ctx, json.RawMessage(`{"city":"不存在的小城"}`))
	if err == nil {
		t.Fatal("未收录城市应报错")
	}
	if _, err := w.Run(ctx, json.RawMessage(`{"city_code":"123"}`)); err == nil {
		t.Fatal("非法 city_code 应报错")
	}
}
