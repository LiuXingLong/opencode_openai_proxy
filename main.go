package main

import (
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/LiuXingLong/opencode-openai-proxy/config"
	"github.com/LiuXingLong/opencode-openai-proxy/handler"
	"github.com/LiuXingLong/opencode-openai-proxy/logger"
	"github.com/LiuXingLong/opencode-openai-proxy/middleware"
	"github.com/LiuXingLong/opencode-openai-proxy/proxy"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.Load()

	l, err := logger.Init(cfg.LogFile)
	if err != nil {
		log.Fatalf("init logger failed: %v", err)
	}
	slog.SetDefault(l)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Trace())
	r.Use(middleware.Auth())

	p := proxy.New(cfg.UpstreamBaseURL)
	h := handler.NewResponsesHandler(p)

	r.POST("/v1/responses", h.Create)
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
		"log_file", cfg.LogFile,
	)

	if err := r.Run(cfg.ListenAddr); err != nil {
		l.Error("server failed", "error", err.Error())
		os.Exit(1)
	}
}
