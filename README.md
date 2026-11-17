# api-server

自建**上游接口服务**：提供爬虫接口与厂商 API 包装接口，作为已部署聚合站 apiHub 的能力提供方。apiHub 经 `127.0.0.1` 内网转发到本服务，本服务不暴露公网端口。

> **工程规范（硬性）**：[docs/CONVENTIONS.md](docs/CONVENTIONS.md) —— 优先级 **响应速度 > 稳定健壮 > 其他**；能异步的不进主流程。新增/修改接口前必读，并逐项过其第 4 节检查单。

```
用户 → apiHub(已部署) ──127.0.0.1:8300──▶ api-server
                                            ├─ 鉴权 + 限流（每密钥令牌桶）
                                            ├─ POST /api/v1/fetch/:name  爬虫接口（并发闸门+TTL缓存）
                                            ├─ POST /api/v1/vendor/:name 厂商包装（重试+错误映射）
                                            ├─ GET  /api/v1/meta         能力清单
                                            ├─ GET  /api/v1/stats        用量统计（admin）
                                            └─ SQLite（日志/缓存，单文件）
```

技术栈：Go + Gin + colly/goquery + SQLite（modernc 纯 Go 驱动，免 cgo 可交叉编译）。目标环境：蓝队云小服务器（与其他项目混部），内存占用常态 ~30MB，systemd `MemoryMax=300M` 兜底。

## 快速开始（本地）

```bash
cp configs/config.example.yaml config.yaml   # 改一下密钥
go run ./cmd/server -dev
```

```bash
curl http://127.0.0.1:8300/healthz
curl -s -X POST http://127.0.0.1:8300/api/v1/fetch/pageinfo \
  -H "X-API-Key: change-me-apihub-key" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com"}'
curl -s -X POST http://127.0.0.1:8300/api/v1/fetch/weather \
  -H "X-API-Key: change-me-apihub-key" \
  -H "Content-Type: application/json" \
  -d '{"city":"北京"}'
curl -s -X POST http://127.0.0.1:8300/api/v1/vendor/github \
  -H "X-API-Key: change-me-apihub-key" \
  -d '{"owner":"golang","repo":"go"}'
curl -s "http://127.0.0.1:8300/api/v1/stats?days=7" -H "X-API-Key: change-me-admin-key"
```

## 统一响应

```json
{"code": 0, "message": "ok", "data": {}, "request_id": "1a2b3c4d..."}
```

| 业务码 | HTTP | 含义 |
|---|---|---|
| 0 | 200 | 成功 |
| 40001 | 400 | 参数错误 |
| 40101 | 401 | 无效/缺失 API Key |
| 40301 | 403 | 需要 admin 密钥 |
| 40004 | 404 | 未知的 spider/vendor |
| 42901 | 429 | 触发限流 |
| 42902 | 429 | 任务并发槽位满，稍后重试 |
| 50000 | 500 | 服务内部错误 |
| 50011 | 504 | 抓取/上游任务超时 |
| 50012 | 502 | 目标响应解析失败 |
| 50013 | 502 | 目标站/上游请求失败 |
| 50014 | 404 | 上游资源不存在 |

## 面向小服务器的设计约束

1. **资源闸门**：所有抓取/上游任务共用并发信号量（`max_concurrency`，默认 4）+ 单任务硬超时（`timeout_seconds`，默认 20s）+ 槽位等待上限（`queue_timeout_seconds`，超时返回 42902）。
2. **两级缓存即节流**：爬虫结果按「爬虫名+请求体」哈希做 TTL 缓存。L1 内存（微秒级命中，1024 条/单条 ≤256KB 上限），L2 SQLite（重启后仍热，重启不重复抓取）。响应头 `X-Cache: hit/miss`。
3. **故障隔离**：任一 handler panic 被 Recovery 捕获返回 50000，进程不倒；爬虫有 SSRF 防护（禁止解析到内网/保留地址，禁止跨域重定向）；weather 双数据源并行抓取、单源失败自动降级、瞬态错误重试一次。
4. **连接与超时治理**：HTTP server 全套超时（ReadHeader/Read/Write/Idle + 16KB 头上限）防慢客户端占用；上游请求共享 keep-alive 连接池（免重复 TLS 握手）；访问日志批量事务落盘（64 条/500ms）降低 SQLite 写放大。
5. **可观测**：`/healthz` 免鉴权输出 uptime、在途任务数（tasks_in_flight）、L1 缓存条目数（cache_l1_entries）；`/api/v1/stats` 聚合调用量/错误率/延迟/缓存命中。
6. **不做 JS 渲染**：无头浏览器内存吃不起，预留后续按需单点接入。

### 实测参考（本机开发机）

- 冷调用（真实抓取中国天气网，双源并行）：~0.8s
- 热调用（L1 命中）：15~20ms 端到端（服务端亚毫秒）
- 重启后首调（L2 命中）：~16ms，不重复抓取

## 如何新增能力

**新增爬虫**：在 `internal/scraper/spiders/` 新建文件，实现 `scraper.Spider` 接口（`Name/Desc/Run`），错误用 `scraper.ErrBadRequest/ErrTimeout/ErrFetch/ErrParse`，然后在 `cmd/server/main.go` 注册一行。

**内置爬虫**：

- `weather` — 中国天气网（weather.com.cn，气象局官方公众服务）天气预报。输入 `{"city":"北京"}`（内置 39 个直辖市/省会/主要城市映射）或 `{"city_code":"101010100"}`（9 位代码，见城市页 URL）。返回：实况（温度/风向风力/湿度/气压/能见度/降雨/AQI）+ 7 天预报 + 30 项生活指数 + 气象预警。HTML 预报失败自动回落 JSON 逐日数据；默认缓存 5 分钟，对源站友好
- `pageinfo` — 抓取单个网页，返回标题/描述/正文摘要/链接列表（含 SSRF 防护）
- `goldprice` — 黄金/白银实时行情（新浪财经，GBK 解码）。输入 `{"codes":["hf_XAU"]}` 可选（hf_XAU 伦敦金/hf_XAG 伦敦银/hf_GC COMEX黄金/hf_SI COMEX白银/gds_AUTD 上海金延期）
- `hotsearch` — 多平台热搜榜。输入 `{"platform":"douyin|zhihu|bilibili","limit":20}`，返回排名/标题/热度/来源链接（微博源有 cookie 反爬，攻坚中）
- `epicfree` — Epic 商城免费游戏（当前免费 + 即将免费，含原价/封面/商店链接）。无参数
- `hitokoto` — 一言，多分类。输入 `{"c":["a","i"]}`（代码）或 `{"type":"诗词"}`；分类：动画/漫画/文学/原创/网络/其他/影视/网易云/诗词/哲学。随机语义不走缓存（X-Cache: none）
- `horoscope` — 星座今日运势（星座屋）。输入 `{"astro":"aries"}` 或 `{"astro":"白羊座"}`，返回星级评分/健康商谈指数/幸运颜色/幸运数字/速配星座/运势概述
- `todayhistory` — 历史上的今天（百度百科）。输入 `{"date":"11-17"}` 可选（默认今天），按年份升序返回事件列表（含百科链接）
- `qqavatar` — QQ头像直链（腾讯官方接口，零上游请求）。输入 `{"qq":"10000","size":640}`
- `bing` — 必应每日壁纸（官方归档）。输入 `{"days":7}`（1-8），返回日期/标题/直链/版权
- `randompic` — 随机图片。输入 `{"type":"scenery|acg","width":1920,"height":1080}`（尺寸仅风景生效），服务端解析出可直连的图片地址；随机语义不走缓存

**新增厂商**：在 `internal/provider/adapters/` 新建文件，实现 `provider.Vendor` 接口；密钥从环境变量读取，失败重试退避，上游错误码映射为本服务业务码。可复制 `adapters/example.go`（GitHub 适配器）作模板。

## 部署到服务器

```bash
# 本机（Git Bash）
./scripts/build.sh
# 产出 dist/api-server-linux-amd64

# 服务器
scp dist/api-server-linux-amd64 user@server:/tmp/
ssh user@server
sudo useradd -r -s /usr/sbin/nologin apiserver
sudo mkdir -p /opt/api-server && sudo mv /tmp/api-server-linux-amd64 /opt/api-server/api-server
sudo chmod +x /opt/api-server/api-server && sudo chown -R apiserver:apiserver /opt/api-server
# 放配置（从 configs/config.example.yaml 改，务必换随机长密钥）
sudo vim /opt/api-server/config.yaml
sudo cp deploy/api-server.service /etc/systemd/system/   # 在仓库目录执行，或先 scp
sudo systemctl daemon-reload && sudo systemctl enable --now api-server
journalctl -u api-server -f
```

验证：`curl http://127.0.0.1:8300/healthz`

## apiHub 挂渠道指引

在 apiHub 新增一个上游渠道指向本服务：

- 地址：`http://127.0.0.1:8300`（同机内网，不出公网）
- 鉴权：配置 `config.yaml` 中 apihub 密钥（建议部署时换成 32+ 位随机串）
- 能力发现：`GET /api/v1/meta` 返回全部可用爬虫/厂商及说明，方便对接

后续升级：`./scripts/build.sh` → `scp` → `systemctl restart api-server`，SQLite 数据文件不受影响。

## 配置说明

见 [configs/config.example.yaml](configs/config.example.yaml)，全部字段有默认值，最小配置只需替换两个密钥。真实 `config.yaml` 已被 `.gitignore` 排除，不要提交密钥。

## 状态

- [x] 骨架：配置/SQLite/缓存/鉴权/限流/访问日志/优雅关停
- [x] 爬虫：`pageinfo`（网页摘要）、`weather`（中国天气网天气预报）
- [x] 厂商适配器模板：`github`
- [ ] 按业务需要扩充爬虫与厂商适配器
- [ ]（预留）JS 渲染支持、Web 管理界面
