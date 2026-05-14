package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/LiuXingLong/opencode-openai-proxy/logger"
	"log/slog"
)

func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := uuid.New().String()
		l := slog.Default().With("trace_id", traceID)
		c.Request = c.Request.WithContext(logger.NewContext(c.Request.Context(), l))
		c.Header("X-Trace-Id", traceID)
		c.Next()
	}
}
