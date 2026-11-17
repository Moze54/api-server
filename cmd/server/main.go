// api-server：自建上游接口服务（爬虫 + 厂商包装），供 apiHub 内网调用。
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"api-server/internal/cache"
	"api-server/internal/config"
	"api-server/internal/handler"
	"api-server/internal/middleware"
	"api-server/internal/provider"
	"api-server/internal/provider/adapters"
	"api-server/internal/scraper"
	"api-server/internal/scraper/spiders"
	"api-server/internal/store"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	var (
		configPath = flag.String("config", "config.yaml", "配置文件路径")
		dev        = flag.Bool("dev", false, "开发模式（保留 gin debug 日志）")
	)
	flag.Parse()

	if !*dev {
		gin.SetMode(gin.ReleaseMode)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("加载配置失败", "err", err)
		os.Exit(1)
	}

	st, err := store.Open(cfg.Store.Path)
	if err != nil {
		logger.Error("打开数据库失败", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	boot := time.Now()
	ttlCache := cache.New(st.DB)
	defer ttlCache.Close() // 关停前排空 L2 异步写（先于 st.Close 执行）
	accessLog := middleware.NewAccessLog(st, logger)

	// 注册能力：新增爬虫/厂商时在此添加一行
	scraperReg := scraper.NewRegistry()
	must(scraperReg.Register(&spiders.PageInfo{UserAgent: cfg.Scraper.UserAgent}))
	must(scraperReg.Register(&spiders.Weather{UserAgent: cfg.Scraper.UserAgent}))
	must(scraperReg.Register(&spiders.GoldPrice{UserAgent: cfg.Scraper.UserAgent}))
	must(scraperReg.Register(&spiders.EpicFree{}))
	must(scraperReg.Register(&spiders.HotSearch{UserAgent: cfg.Scraper.UserAgent}))
	must(scraperReg.Register(&spiders.QQAvatar{}))
	must(scraperReg.Register(&spiders.Bing{UserAgent: cfg.Scraper.UserAgent}))
	must(scraperReg.Register(&spiders.RandomPic{UserAgent: cfg.Scraper.UserAgent}))
	must(scraperReg.Register(&spiders.Hitokoto{UserAgent: cfg.Scraper.UserAgent}))
	must(scraperReg.Register(&spiders.Horoscope{UserAgent: cfg.Scraper.UserAgent}))
	must(scraperReg.Register(&spiders.TodayHistory{UserAgent: cfg.Scraper.UserAgent}))
	vendorReg := provider.NewRegistry()
	must(vendorReg.Register(adapters.NewGitHub()))

	pool := scraper.NewPool(cfg.Scraper.MaxConcurrency,
		cfg.Server.TimeoutSeconds, cfg.Scraper.QueueTimeoutSeconds)

	router := gin.New()
	// 全局：请求 ID → 访问日志（defer 记录，Recovery 之内，保证 panic 也被记录）→ panic 恢复
	router.Use(
		middleware.RequestID(),
		accessLog.Middleware(),
		middleware.Recovery(logger),
	)

	router.GET("/healthz", handler.Health(boot, pool, ttlCache)) // 免鉴权，供探活

	// 业务路由：鉴权 + 限流只作用于 /api/v1
	api := router.Group("/api/v1",
		middleware.NewAuthenticator(cfg.Auth).Middleware(),
		middleware.NewRateLimiter(cfg.RateLimit.PerKeyRPS, cfg.RateLimit.Burst).Middleware(),
	)
	api.GET("/meta", handler.Meta(scraperReg, vendorReg))
	api.POST("/fetch/:name", handler.Scrape(scraperReg, pool, ttlCache,
		time.Duration(cfg.Scraper.DefaultCacheTTLSeconds)*time.Second))
	api.POST("/vendor/:name", handler.Vendor(vendorReg, pool))
	api.GET("/stats", middleware.RequireAdmin(), handler.Stats(st))

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,  // 请求体读取（≤1MB）
		WriteTimeout:      35 * time.Second,  // > 任务超时(20s)+槽位等待(10s)，慢客户端不长期占连接
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		logger.Info("api-server 已启动", "addr", cfg.Server.Addr, "spiders", len(scraperReg.List()), "vendors", len(vendorReg.List()))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("监听失败", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("收到退出信号，开始优雅关停")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("HTTP 关停超时", "err", err)
	}
	accessLog.Close() // 排空访问日志队列
	logger.Info("api-server 已停止")
}

func must(err error) {
	if err != nil {
		slog.Error("注册能力失败", "err", err)
		os.Exit(1)
	}
}
