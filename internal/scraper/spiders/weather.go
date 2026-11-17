// Weather 中国天气网（weather.com.cn，中国气象局官方公众服务）天气爬虫。
// 数据端点（2026-11 实测，均为 UTF-8，无需转码）：
//   - https://d1.weather.com.cn/weather_index/{code}.html  实况 + 生活指数 + 预警 + 逐日(JSON 变量)
//   - https://www.weather.com.cn/weather/{code}.shtml      7 天预报(HTML)
// 两个端点都必须带 Referer: http://www.weather.com.cn/，否则拒绝访问。
// 城市代码：内置主要城市映射；其他城市让调用方直接传 city_code
// （城市页 URL weather/{code}.shtml 中的 9 位数字即代码）。
package spiders

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"api-server/internal/scraper"
)

type Weather struct {
	UserAgent string
}

func (w *Weather) Name() string { return "weather" }
func (w *Weather) Desc() string {
	return `中国天气网：实况 + 7天预报 + 生活指数 + 预警。输入: {"city":"北京"} 或 {"city_code":"101010100"}`
}

// builtinCityCodes 内置直辖市/省会/主要城市 → 中国天气网城市代码。
var builtinCityCodes = map[string]string{
	"北京": "101010100", "上海": "101020100", "天津": "101030100", "重庆": "101040100",
	"广州": "101280101", "深圳": "101280601", "杭州": "101210101", "南京": "101190101",
	"苏州": "101190401", "成都": "101270101", "武汉": "101200101", "西安": "101110101",
	"郑州": "101180101", "济南": "101120101", "青岛": "101120201", "沈阳": "101070101",
	"大连": "101070201", "哈尔滨": "101050101", "长春": "101060101", "石家庄": "101090101",
	"太原": "101100101", "合肥": "101220101", "南昌": "101240101", "福州": "101230101",
	"厦门": "101230201", "长沙": "101250101", "南宁": "101300101", "海口": "101310101",
	"贵阳": "101260101", "昆明": "101290101", "拉萨": "101140101", "兰州": "101160101",
	"西宁": "101150101", "银川": "101170101", "乌鲁木齐": "101130101", "呼和浩特": "101080101",
	"香港": "101320101", "澳门": "101330101", "台北": "101340101",
}

// wcText 天气代码 → 中文，fc 兜底路径用（官方代码表 00-31）。
var wcText = map[string]string{
	"00": "晴", "01": "多云", "02": "阴", "03": "阵雨", "04": "雷阵雨",
	"05": "雷阵雨伴有冰雹", "06": "雨夹雪", "07": "小雨", "08": "中雨", "09": "大雨",
	"10": "暴雨", "11": "大暴雨", "12": "特大暴雨", "13": "阵雪", "14": "小雪",
	"15": "中雪", "16": "大雪", "17": "暴雪", "18": "雾", "19": "冻雨",
	"20": "沙尘暴", "21": "小雨-中雨", "22": "中雨-大雨", "23": "大雨-暴雨",
	"24": "暴雨-大暴雨", "25": "大暴雨-特大暴雨", "26": "小雪-中雪", "27": "中雪-大雪",
	"28": "大雪-暴雪", "29": "浮尘", "30": "扬沙", "31": "强沙尘暴",
}

type weatherInput struct {
	City     string `json:"city"`      // 城市中文名（查内置表）
	CityCode string `json:"city_code"` // 中国天气网 9 位城市代码，优先于 city
}

type weatherCurrent struct {
	Weather       string `json:"weather"`
	Temp          string `json:"temp"`
	WindDirection string `json:"wind_direction"`
	WindPower     string `json:"wind_power"`
	Humidity      string `json:"humidity"`
	Pressure      string `json:"pressure_hpa"`
	Visibility    string `json:"visibility"`
	Rain24h       string `json:"rain_24h"`
	AQI           string `json:"aqi"`
	ObserveTime   string `json:"observe_time"`
	Date          string `json:"date"`
}

type weatherDay struct {
	Day       string `json:"day"`
	Weather   string `json:"weather"`
	High      string `json:"high"`
	Low       string `json:"low"`
	Wind      string `json:"wind"`
	WindLevel string `json:"wind_level"`
}

type lifeIndex struct {
	Name string `json:"name"`
	Hint string `json:"hint"`
	Desc string `json:"desc"`
}

type weatherOutput struct {
	City           string          `json:"city"`
	CityCode       string          `json:"city_code"`
	UpdatedAt      int64           `json:"updated_at"`
	Current        *weatherCurrent `json:"current,omitempty"`
	Forecast       []weatherDay    `json:"forecast"`
	ForecastSource string          `json:"forecast_source"` // html（主）/ fc（兜底）
	Alarms         any             `json:"alarms,omitempty"`
	LifeIndexes    []lifeIndex     `json:"life_indexes"`

	fcData map[string]any `json:"-"` // 实况端点里的 fc 逐日数据，HTML 失败时兜底用
}

// 实况端点是 `var NAME = {...};` 形式的 JS 变量，按名提取。
var (
	dataSKRe   = regexp.MustCompile(`(?s)var\s+dataSK\s*=\s*(\{.*?\});`)
	dataZSRe   = regexp.MustCompile(`(?s)var\s+dataZS\s*=\s*(\{.*?\});`)
	alarmDZRe  = regexp.MustCompile(`(?s)var\s+alarmDZ\s*=\s*(\{.*?\});`)
	fcRe       = regexp.MustCompile(`(?s)var\s+fc\s*=\s*(\{.*?\});`)
	cityCodeRe = regexp.MustCompile(`^\d{9}$`)
)

// weatherHTTPClient 包级共享客户端：keep-alive 连接复用，避免每次请求重新 TLS 握手。
var weatherHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	},
}

// httpStatusError 上游返回非 200 状态码。
type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return "HTTP " + strconv.Itoa(e.code) }

// isTransient 是否值得重试：传输层错误与 5xx 重试；4xx、调用方取消/超时不重试。
func isTransient(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var hs *httpStatusError
	if errors.As(err, &hs) {
		return hs.code >= 500
	}
	return true
}

func errText(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}

func (w *Weather) Run(ctx context.Context, raw json.RawMessage) (any, error) {
	var in weatherInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, scraper.ErrBadRequest("请求体必须是 JSON 对象")
	}
	code := in.CityCode
	if code != "" {
		if !cityCodeRe.MatchString(code) {
			return nil, scraper.ErrBadRequest("city_code 必须是 9 位数字的中国天气网城市代码，如 101010100")
		}
	} else if name := strings.TrimSpace(in.City); name != "" {
		c, ok := builtinCityCodes[name]
		if !ok {
			return nil, scraper.ErrBadRequest(
				"城市 \"" + name + "\" 未收录（内置表只含直辖市/省会/主要城市），请直接传 city_code：" +
					"在 weather.com.cn 搜索该城市，其页面 URL weather/xxxxxxxxx.shtml 中的 9 位数字即代码")
		}
		code = c
	} else {
		return nil, scraper.ErrBadRequest("city 或 city_code 必填")
	}

	out := weatherOutput{
		City:       in.City,
		CityCode:   code,
		UpdatedAt:  time.Now().Unix(),
		Forecast:   []weatherDay{},
		LifeIndexes: []lifeIndex{},
	}

	// 两路并行抓取：实况 JSON + 7 天 HTML，总延迟取决于较慢的一路
	idxBody, htmlBody, idxErr, htmlErr := w.fetchBoth(ctx, code)

	// 1) 实况 + 生活指数 + 预警 + fc 兜底数据（失败不致命，可降级）
	if idxBody != nil {
		s := string(idxBody)
		if sk := extractJSONObject(dataSKRe, s); sk != nil {
			out.Current = parseCurrent(sk)
			out.City = firstNonEmpty(out.City, str(sk, "cityname"))
		}
		if zs := extractJSONObject(dataZSRe, s); zs != nil {
			out.LifeIndexes = parseLifeIndexes(zs)
		}
		if al := extractJSONObject(alarmDZRe, s); al != nil {
			if wArr, ok := al["w"]; ok {
				out.Alarms = wArr
			}
		}
		if fc := extractJSONObject(fcRe, s); fc != nil {
			out.fcData = fc // HTML 失败时的逐日兜底
		}
	}

	// 2) 7 天预报：HTML 为主，fc 兜底
	var days []weatherDay
	if htmlBody != nil {
		days, _ = parseForecastHTML(htmlBody)
	}
	if len(days) > 0 {
		out.Forecast = days
		out.ForecastSource = "html"
	} else if parsed := parseFC(out.fcData); len(parsed) > 0 {
		out.Forecast = parsed
		out.ForecastSource = "fc"
	}

	// 双源都失败才报错；单源成功则降级返回可用部分
	if out.Current == nil && len(out.Forecast) == 0 {
		return nil, scraper.ErrFetch("中国天气网两个数据源均失败: 实况端点: " +
			errText(idxErr) + "; 预报页: " + errText(htmlErr))
	}
	return out, nil
}

// fetchBoth 并行抓取实况 JSON 与 7 天 HTML 两路。
func (w *Weather) fetchBoth(ctx context.Context, code string) (idx, html []byte, idxErr, htmlErr error) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		idx, idxErr = w.fetchRetry(ctx, "https://d1.weather.com.cn/weather_index/"+code+".html")
	}()
	go func() {
		defer wg.Done()
		html, htmlErr = w.fetchRetry(ctx, "https://www.weather.com.cn/weather/"+code+".shtml")
	}()
	wg.Wait()
	return
}

// fetchRetry 瞬态错误（网络抖动/5xx）重试一次，退避 300ms，受 ctx 约束。
func (w *Weather) fetchRetry(ctx context.Context, url string) ([]byte, error) {
	body, err := w.fetch(ctx, url)
	if err == nil || !isTransient(err) {
		return body, err
	}
	select {
	case <-time.After(300 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return w.fetch(ctx, url)
}

// fetch 带 Referer 的 GET；响应上限 2MB，状态码非 200 返回 httpStatusError。
func (w *Weather) fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "http://www.weather.com.cn/")
	req.Header.Set("User-Agent", w.UserAgent)
	resp, err := weatherHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{code: resp.StatusCode}
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}

// extractJSONObject 用正则提取 `var NAME = {...};` 的 JSON 对象。
func extractJSONObject(re *regexp.Regexp, body string) map[string]any {
	m := re.FindStringSubmatch(body)
	if len(m) < 2 {
		return nil
	}
	var obj map[string]any
	if json.Unmarshal([]byte(m[1]), &obj) != nil {
		return nil
	}
	return obj
}

func str(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// parseCurrent 实况解析；键名缺失时对应字段留空（部分字段官方偶发缺失）。
func parseCurrent(sk map[string]any) *weatherCurrent {
	return &weatherCurrent{
		Weather:       firstNonEmpty(str(sk, "weather"), str(sk, "w")),
		Temp:          str(sk, "temp"),
		WindDirection: firstNonEmpty(str(sk, "WD"), str(sk, "wd")),
		WindPower:     firstNonEmpty(str(sk, "WS"), str(sk, "ws")),
		Humidity:      firstNonEmpty(str(sk, "SD"), str(sk, "sd")),
		Pressure:      str(sk, "qy"),
		Visibility:    str(sk, "njd"),
		Rain24h:       str(sk, "rain24h"),
		AQI:           str(sk, "aqi"),
		ObserveTime:   str(sk, "time"),
		Date:          str(sk, "date"),
	}
}

// parseLifeIndexes dataZS.zs 的扁平键（{k}_name/{k}_hint/{k}_des_s）转结构化列表。
func parseLifeIndexes(zs map[string]any) []lifeIndex {
	z, ok := zs["zs"].(map[string]any)
	if !ok {
		return nil
	}
	bases := make([]string, 0, 32)
	for k := range z {
		if strings.HasSuffix(k, "_name") {
			bases = append(bases, strings.TrimSuffix(k, "_name"))
		}
	}
	sort.Strings(bases)
	out := make([]lifeIndex, 0, len(bases))
	for _, b := range bases {
		name := str(z, b+"_name")
		if name == "" {
			continue
		}
		out = append(out, lifeIndex{Name: name, Hint: str(z, b+"_hint"), Desc: str(z, b+"_des_s")})
	}
	return out
}

// parseForecastHTML 解析 7 天页的 ul.t.clearfix > li 结构。
func parseForecastHTML(body []byte) ([]weatherDay, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	days := make([]weatherDay, 0, 7)
	doc.Find("ul.t.clearfix > li").EachWithBreak(func(_ int, li *goquery.Selection) bool {
		day := strings.TrimSpace(li.Find("h1").First().Text())
		if day == "" {
			return true
		}
		tem := li.Find("p.tem").First()
		win := li.Find("p.win").First()
		dirs := make([]string, 0, 2)
		win.Find("em span[title]").Each(func(_ int, s *goquery.Selection) {
			if t, ok := s.Attr("title"); ok && strings.TrimSpace(t) != "" {
				dirs = append(dirs, strings.TrimSpace(t))
			}
		})
		days = append(days, weatherDay{
			Day:       day,
			Weather:   strings.TrimSpace(li.Find("p.wea").First().Text()),
			High:      strings.TrimSpace(tem.Find("span").First().Text()),
			Low:       strings.TrimSpace(tem.Find("i").First().Text()),
			Wind:      strings.Join(dirs, "转"),
			WindLevel: strings.TrimSpace(win.Find("i").First().Text()),
		})
		return len(days) < 7
	})
	return days, nil
}

// parseFC 用 JSON 端点的 fc 数据兜底拼 7 天预报。
func parseFC(fc map[string]any) []weatherDay {
	if fc == nil {
		return nil
	}
	items, ok := fc["f"].([]any)
	if !ok {
		return nil
	}
	days := make([]weatherDay, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		day := firstNonEmpty(str(m, "fj"), str(m, "fi"))
		if fi, fj := str(m, "fi"), str(m, "fj"); fi != "" && fj != "" && fi != fj {
			day = fi + " " + fj
		}
		days = append(days, weatherDay{
			Day:       day,
			Weather:   codeWeather(str(m, "fa"), str(m, "fb")),
			High:      celsius(str(m, "fc")),
			Low:       celsius(str(m, "fd")),
			Wind:      firstNonEmpty(str(m, "fe"), str(m, "ff")),
			WindLevel: firstNonEmpty(str(m, "fg"), str(m, "fh")),
		})
	}
	return days
}

func celsius(v string) string {
	if v == "" {
		return ""
	}
	return v + "℃"
}

// codeWeather 白天/夜间天气代码 → 文本，相同则只出一个。
func codeWeather(day, night string) string {
	dt := wcText[day]
	nt := wcText[night]
	if dt == "" {
		dt = day
	}
	if nt == "" {
		nt = night
	}
	if dt == nt || nt == "" {
		return dt
	}
	if dt == "" {
		return nt
	}
	return dt + "转" + nt
}
