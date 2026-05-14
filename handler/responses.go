package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/LiuXingLong/opencode-openai-proxy/converter"
	"github.com/LiuXingLong/opencode-openai-proxy/logger"
	"github.com/LiuXingLong/opencode-openai-proxy/middleware"
	"github.com/LiuXingLong/opencode-openai-proxy/proxy"
)

type ResponsesHandler struct {
	Proxy *proxy.Proxy
}

func NewResponsesHandler(p *proxy.Proxy) *ResponsesHandler {
	return &ResponsesHandler{Proxy: p}
}

func (h *ResponsesHandler) Create(c *gin.Context) {
	l := logger.FromContext(c.Request.Context())
	start := time.Now()

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		l.Error("read request body failed", "error", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	l.Info("incoming request",
		"method", c.Request.Method,
		"path", c.Request.URL.Path,
		"body", string(body),
	)

	var reqMeta struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	json.Unmarshal(body, &reqMeta)

	chatReqBody, err := converter.ConvertRequest(body)
	if err != nil {
		l.Error("convert request failed", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "conversion failed"})
		return
	}

	authHeader := middleware.GetAuthHeader(c)

	upstreamResp, err := h.Proxy.Send(c.Request.Context(), chatReqBody, authHeader)
	if err != nil {
		l.Error("upstream request failed", "error", err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"error": "upstream request failed"})
		return
	}
	defer upstreamResp.Body.Close()

	if upstreamResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(upstreamResp.Body)
		l.Error("upstream returned error",
			"status", upstreamResp.StatusCode,
			"body", string(respBody),
		)
		c.JSON(upstreamResp.StatusCode, gin.H{
			"error": fmt.Sprintf("upstream error: %s", string(respBody)),
		})
		return
	}

	if reqMeta.Stream {
		h.handleStreaming(c, upstreamResp.Body, reqMeta.Model, start, l)
	} else {
		h.handleNonStreaming(c, upstreamResp.Body, start, l)
	}
}

func (h *ResponsesHandler) handleNonStreaming(c *gin.Context, upstreamBody io.Reader, start time.Time, l *slog.Logger) {
	rawBody, err := io.ReadAll(upstreamBody)
	if err != nil {
		l.Error("read upstream response body failed", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "read upstream response failed"})
		return
	}

	converted, err := converter.ConvertNonStreamingResponse(rawBody)
	if err != nil {
		l.Error("convert response failed", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "response conversion failed"})
		return
	}

	l.Info("outgoing response",
		"status", http.StatusOK,
		"duration", time.Since(start).String(),
		"body", string(converted),
	)

	c.Data(http.StatusOK, "application/json", converted)
}

func (h *ResponsesHandler) handleStreaming(c *gin.Context, upstreamBody io.Reader, model string, start time.Time, l *slog.Logger) {
	state := converter.NewStreamState(model, time.Now().Unix())

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.WriteHeader(http.StatusOK)

	createdEvent := converter.BuildStreamCreatedEvent(state.RespID, state.Model, state.CreatedAt)
	fmt.Fprint(c.Writer, createdEvent)
	c.Writer.Flush()

	scanner := bufio.NewScanner(upstreamBody)
	scanner.Buffer(make([]byte, 0, 1024*64), 1024*1024)

	itemAdded := false

	for scanner.Scan() {
		line := scanner.Text()
		deltaText, finishReason, isDone, upstreamUsage := converter.ParseChatStreamLine(line)

		if isDone {
			break
		}

		if deltaText != "" && !itemAdded {
			itemEvent := converter.BuildStreamItemAddedEvent(state.MsgID, "assistant", state.OutputIndex)
			fmt.Fprint(c.Writer, itemEvent)
			itemAdded = true
		}

		if deltaText != "" {
			state.HasContent = true
			state.AccumulatedText += deltaText
			deltaEvent := converter.BuildStreamTextDeltaEvent(state.MsgID, deltaText, state.OutputIndex, state.ContentIndex)
			fmt.Fprint(c.Writer, deltaEvent)
		}

		if upstreamUsage != nil {
			state.PromptTokens = upstreamUsage.PromptTokens
			state.CompletionTokens = upstreamUsage.CompletionTokens
		}

		if finishReason != "" {
			if state.HasContent {
				doneEvent := converter.BuildStreamTextDoneEvent(state.MsgID, state.AccumulatedText, state.OutputIndex, state.ContentIndex)
				fmt.Fprint(c.Writer, doneEvent)
			}

			status := converter.MapFinishReason(&finishReason)
			completedEvent := converter.BuildStreamCompletedEvent(
				state.RespID, state.Model, state.CreatedAt,
				status, state.MsgID, state.AccumulatedText,
				state.PromptTokens, state.CompletionTokens,
			)
			fmt.Fprint(c.Writer, completedEvent)
		}

		c.Writer.Flush()
	}

	if err := scanner.Err(); err != nil {
		l.Error("stream read error", "error", err.Error())
	}

	l.Info("outgoing response",
		"status", http.StatusOK,
		"duration", time.Since(start).String(),
		"type", "stream",
		"usage", fmt.Sprintf(`{"input_tokens":%d,"output_tokens":%d}`, state.PromptTokens, state.CompletionTokens),
	)
}
