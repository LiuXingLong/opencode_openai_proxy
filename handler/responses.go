package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/LiuXingLong/opencode-openai-proxy/converter"
	"github.com/LiuXingLong/opencode-openai-proxy/logger"
	"github.com/LiuXingLong/opencode-openai-proxy/middleware"
	"github.com/LiuXingLong/opencode-openai-proxy/proxy"
	"github.com/gin-gonic/gin"
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

	upstreamResp, err := h.Proxy.Send(c.Request.Context(), c.Request.URL.Path, chatReqBody, authHeader)
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
		c.Data(upstreamResp.StatusCode, "application/json", respBody)
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

	l.Info("upstream model response",
		"status", http.StatusOK,
		"body", string(rawBody),
	)

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

	// 按 index 记录已添加的 function_call item（避免重复发送 item_added）
	addedFCIndexes := map[int]bool{}
	fcItemIDs := map[int]string{}
	fcOutputIndex := map[int]int{}

	for scanner.Scan() {
		line := scanner.Text()
		deltaText, finishReason, isDone, upstreamUsage, toolCallDeltas := converter.ParseChatStreamLine(line)

		// 记录上游返回的每个非空 chunk
		if deltaText != "" || len(toolCallDeltas) > 0 || finishReason != "" || upstreamUsage != nil {
			l.Info("upstream stream chunk",
				"delta_text", deltaText,
				"tool_call_count", len(toolCallDeltas),
				"finish_reason", finishReason,
				"usage", upstreamUsage,
			)
		}

		if isDone {
			break
		}

		// 处理 tool_calls deltas
		for _, tcd := range toolCallDeltas {
			if !addedFCIndexes[tcd.Index] {
				addedFCIndexes[tcd.Index] = true
				itemID := "fcall_" + uuid.New().String()
				fcItemIDs[tcd.Index] = itemID
				oidx := state.NextOutputIndex()
				fcOutputIndex[tcd.Index] = oidx
				name := tcd.Function.Name
				callID := tcd.ID
				if name == "" && tcd.Type != "" {
					name = tcd.Type
				}
				fcEvent := converter.BuildStreamFunctionCallItemAddedEvent(itemID, name, callID, oidx)
				fmt.Fprint(c.Writer, fcEvent)
			}
			// 发送 arguments delta
			if tcd.Function.Arguments != "" {
				oidx := fcOutputIndex[tcd.Index]
				itemID := fcItemIDs[tcd.Index]
				argEvent := converter.BuildStreamFunctionCallArgumentsDeltaEvent(itemID, tcd.Function.Arguments, oidx)
				fmt.Fprint(c.Writer, argEvent)
			}
			// 累积 arguments
			state.ToolCalls = append(state.ToolCalls, converter.StreamToolCall{
				Index:     tcd.Index,
				ID:        tcd.ID,
				Name:      tcd.Function.Name,
				Arguments: tcd.Function.Arguments,
			})
		}

		// 处理文本 delta
		if deltaText != "" {
			if !state.ItemAdded {
				state.MsgID = "msg_" + uuid.New().String()
				state.TextOutputIndex = state.NextOutputIndex()
				itemEvent := converter.BuildStreamItemAddedEvent(state.MsgID, "assistant", state.TextOutputIndex)
				fmt.Fprint(c.Writer, itemEvent)
				state.ItemAdded = true
			}
			state.HasContent = true
			state.AccumulatedText += deltaText
			deltaEvent := converter.BuildStreamTextDeltaEvent(state.MsgID, deltaText, state.TextOutputIndex, 0)
			fmt.Fprint(c.Writer, deltaEvent)
		}

		if upstreamUsage != nil {
			state.PromptTokens = upstreamUsage.PromptTokens
			state.CompletionTokens = upstreamUsage.CompletionTokens
		}

		if finishReason != "" {
			status := converter.MapFinishReason(&finishReason)

			// 构建所有 output items
			var output []map[string]interface{}

			if state.HasContent {
				doneEvent := converter.BuildStreamTextDoneEvent(state.MsgID, state.AccumulatedText, state.TextOutputIndex, 0)
				fmt.Fprint(c.Writer, doneEvent)

				contentArr := []map[string]interface{}{}
				if state.AccumulatedText != "" {
					contentArr = append(contentArr, map[string]interface{}{
						"type":        "output_text",
						"text":        state.AccumulatedText,
						"annotations": []interface{}{},
					})
				}
				msgItem := map[string]interface{}{
					"id":      state.MsgID,
					"type":    "message",
					"role":    "assistant",
					"content": contentArr,
				}
				output = append(output, msgItem)

				// 发送 response.output_item.done 给 message
				itemDoneEvent := converter.BuildStreamOutputItemDoneEvent(state.MsgID, state.TextOutputIndex, msgItem)
				fmt.Fprint(c.Writer, itemDoneEvent)
			}

			// 合并 tool call arguments 并构建 function_call output items
			type fcMerge struct {
				ID        string
				Index     int
				ItemID    string
				Name      string
				Arguments string
			}
			mergeMap := map[int]*fcMerge{}
			for _, tc := range state.ToolCalls {
				if mergeMap[tc.Index] == nil {
					mergeMap[tc.Index] = &fcMerge{
						Index:  tc.Index,
						ID:     tc.ID,
						ItemID: fcItemIDs[tc.Index],
						Name:   tc.Name,
					}
				}
				mergeMap[tc.Index].Arguments += tc.Arguments
				if tc.ID != "" {
					mergeMap[tc.Index].ID = tc.ID
				}
				if tc.Name != "" {
					mergeMap[tc.Index].Name = tc.Name
				}
			}
			for _, m := range mergeMap {
				callID := m.ID
				if callID == "" {
					callID = "call_" + uuid.New().String()
				}
				itemID := m.ItemID
				if itemID == "" {
					itemID = "fcall_" + uuid.New().String()
				}
				oidx := fcOutputIndex[m.Index]

				// 发送 function_call_arguments.done
				argDone := converter.BuildStreamFunctionCallArgumentsDoneEvent(itemID, m.Arguments, oidx)
				fmt.Fprint(c.Writer, argDone)

				fcItem := map[string]interface{}{
					"id":        itemID,
					"type":      "function_call",
					"call_id":   callID,
					"name":      m.Name,
					"arguments": m.Arguments,
					"status":    "completed",
				}
				output = append(output, fcItem)

				// 发送 response.output_item.done 给 function_call
				itemDoneEvent := converter.BuildStreamOutputItemDoneEvent(itemID, oidx, fcItem)
				fmt.Fprint(c.Writer, itemDoneEvent)
			}

			l.Info("sending response.completed",
				"output", output,
				"usage", fmt.Sprintf(`{"input_tokens":%d,"output_tokens":%d}`, state.PromptTokens, state.CompletionTokens),
			)

			completedEvent := converter.BuildStreamCompletedEvent(
				state.RespID, state.Model, state.CreatedAt,
				status, output,
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
		"model", state.Model,
		"text_length", len(state.AccumulatedText),
		"has_text", state.HasContent,
		"tool_call_count", len(state.ToolCalls),
		"usage", fmt.Sprintf(`{"input_tokens":%d,"output_tokens":%d}`, state.PromptTokens, state.CompletionTokens),
	)
}
