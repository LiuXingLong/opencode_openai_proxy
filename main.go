package main

import (
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"time"

	"github.com/LiuXingLong/opencode-openai-proxy/config"
	"github.com/LiuXingLong/opencode-openai-proxy/handler"
	"github.com/LiuXingLong/opencode-openai-proxy/logger"
	"github.com/LiuXingLong/opencode-openai-proxy/middleware"
	"github.com/LiuXingLong/opencode-openai-proxy/proxy"
	"github.com/LiuXingLong/opencode-openai-proxy/searcher"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()

	cfg := config.Load()

	l, err := logger.Init(cfg.LogFile, cfg.LogLevel)
	if err != nil {
		log.Fatalf("init logger failed: %v", err)
	}
	slog.SetDefault(l)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Trace())
	r.Use(middleware.Auth())

	p := proxy.New(cfg.UpstreamBaseURL, cfg.RouteMap)
	s := searcher.New(cfg.SearchResultCount, time.Duration(cfg.SearchTimeout)*time.Second, cfg.SearchBingURL, cfg.SearchConcurrency)
	h := handler.NewResponsesHandler(p, s, cfg.SearchRetryCount)

	// 注册路由表中的路径 + 默认 /v1/responses
	registered := map[string]bool{"/v1/responses": true}
	for path := range cfg.RouteMap {
		registered[path] = true
	}
	for path := range registered {
		r.POST(path, h.Create)
	}
	r.GET("/health", handler.Health)

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGHUP)
		for range sig {
			if err := logger.ReopenLog(); err != nil {
				l.Error("reopen log failed", "error", err.Error())
			} else {
				l.Info("log file reopened")
			}
		}
	}()

	l.Info("server starting",
		"addr", cfg.ListenAddr,
		"upstream", cfg.UpstreamBaseURL,
		"routes", cfg.RouteMap,
		"log_file", cfg.LogFile,
	)

	if err := r.Run(cfg.ListenAddr); err != nil {
		l.Error("server failed", "error", err.Error())
		os.Exit(1)
	}
}
