package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/LiuXingLong/opencode-openai-proxy/converter"
	"github.com/LiuXingLong/opencode-openai-proxy/logger"
	"github.com/LiuXingLong/opencode-openai-proxy/middleware"
	"github.com/LiuXingLong/opencode-openai-proxy/proxy"
	"github.com/LiuXingLong/opencode-openai-proxy/searcher"
	"github.com/gin-gonic/gin"
)

type ResponsesHandler struct {
	Proxy            *proxy.Proxy
	Searcher         *searcher.Searcher
	retryCount       int
	searxngSummarize bool
}

func NewResponsesHandler(p *proxy.Proxy, s *searcher.Searcher, retryCount int, searxngSummarize bool) *ResponsesHandler {
	return &ResponsesHandler{Proxy: p, Searcher: s, retryCount: retryCount, searxngSummarize: searxngSummarize}
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

	var reqMeta struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	json.Unmarshal(body, &reqMeta)

	l.Info("incoming request",
		"method", c.Request.Method,
		"path", c.Request.URL.Path,
		"model", reqMeta.Model,
		"stream", reqMeta.Stream,
		"body", string(body),
	)

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
		h.handleStreaming(c, upstreamResp.Body, chatReqBody, authHeader, start, l, reqMeta.Model)
	} else {
		h.handleNonStreaming(c, upstreamResp.Body, chatReqBody, authHeader, start, l)
	}
}

func (h *ResponsesHandler) handleNonStreaming(c *gin.Context, upstreamBody io.Reader, originalBody []byte, authHeader string, start time.Time, l *slog.Logger) {
	rawBody, err := io.ReadAll(upstreamBody)
	if err != nil {
		l.Error("read upstream response body failed", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "read upstream response failed"})
		return
	}

	toolCallID, query := extractWebSearchToolCall(rawBody)
	if toolCallID != "" {
		if h.Searcher.Backend() == "searxng" {
			h.handleSearXNGNonStreaming(c, rawBody, query, toolCallID, start, l, originalBody, authHeader)
			return
		}

		var finalMsgText string
		totalAttempts := h.retryCount + 1

		for attempt := 0; attempt < totalAttempts; attempt++ {
			if attempt > 0 {
				l.Info("search: retrying", "query", query, "attempt", attempt+1, "max", totalAttempts)
			}

			results := h.Searcher.Search(c.Request.Context(), query)

			if len(results) > 0 {
				searchTime := time.Now().Format("2006-01-02 15:04")
				resultsJSON, _ := json.Marshal(map[string]interface{}{
					"query":       query,
					"search_time": searchTime,
					"results":     results,
				})
				l.Info("search: feeding results to model",
					"query", query,
					"result_count", len(results),
					"results_json", string(resultsJSON),
				)

				reInvokeBody, err := converter.BuildReInvokeRequest(originalBody, query, resultsJSON, toolCallID)
				if err == nil {
					reInvokeResp, err := h.Proxy.Send(c.Request.Context(), c.Request.URL.Path, reInvokeBody, authHeader)
					if err == nil && reInvokeResp.StatusCode == http.StatusOK {
						reInvokeRaw, _ := io.ReadAll(reInvokeResp.Body)
						reInvokeResp.Body.Close()
						finalMsgText = extractContentFromResponse(reInvokeRaw)
						if finalMsgText != "" && !strings.HasPrefix(finalMsgText, "SEARCH_RESULT_INSUFFICIENT") {
							break
						}
						if strings.HasPrefix(finalMsgText, "SEARCH_RESULT_INSUFFICIENT") {
							l.Info("search: model reported insufficient results",
								"query", query, "attempt", attempt, "response", finalMsgText)
						}
					}
				}
			}
		}

		l.Info("search: re-invoke response",
			"query", query,
			"response_text", finalMsgText,
		)

		converted := buildSearchResponse(rawBody, finalMsgText)
		var respMeta struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		}
		json.Unmarshal(converted, &respMeta)
		l.Info("outgoing response",
			"resp_id", respMeta.ID,
			"model", respMeta.Model,
			"status", http.StatusOK,
			"duration", time.Since(start).String(),
			"body", string(converted),
		)
		c.Data(http.StatusOK, "application/json", converted)
		return
	}

	converted, err := converter.ConvertNonStreamingResponse(rawBody)
	if err != nil {
		l.Error("convert response failed", "error", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "response conversion failed"})
		return
	}

	var respMeta struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	}
	json.Unmarshal(converted, &respMeta)
	l.Info("outgoing response",
		"resp_id", respMeta.ID,
		"model", respMeta.Model,
		"status", http.StatusOK,
		"duration", time.Since(start).String(),
		"body", string(converted),
	)

	c.Data(http.StatusOK, "application/json", converted)
}

func (h *ResponsesHandler) handleStreaming(c *gin.Context, upstreamBody io.Reader, originalBody []byte, authHeader string, start time.Time, l *slog.Logger, model string) {
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

	addedFCIndexes := map[int]bool{}
	fcItemIDs := map[int]string{}
	fcOutputIndex := map[int]int{}
	fcIsBuiltin := map[int]bool{}

	for scanner.Scan() {
		line := scanner.Text()
		deltaText, finishReason, isDone, upstreamUsage, toolCallDeltas := converter.ParseChatStreamLine(line)

		if deltaText != "" || len(toolCallDeltas) > 0 || finishReason != "" || upstreamUsage != nil {
			l.Debug("upstream stream chunk",
				"delta_text", deltaText,
				"tool_call_count", len(toolCallDeltas),
				"finish_reason", finishReason,
				"usage", upstreamUsage,
			)
		}

		if isDone {
			break
		}

		for _, tcd := range toolCallDeltas {
			if !addedFCIndexes[tcd.Index] {
				addedFCIndexes[tcd.Index] = true
				oidx := state.NextOutputIndex()
				fcOutputIndex[tcd.Index] = oidx
				name := tcd.Function.Name
				callID := tcd.ID
				if name == "" && tcd.Type != "" {
					name = tcd.Type
				}
				isBuiltin := converter.IsBuiltinTool(name)
				fcIsBuiltin[tcd.Index] = isBuiltin
				if isBuiltin {
					var itemID string
					if h.Searcher.Backend() == "searxng" && tcd.ID != "" {
						itemID = "ws_" + strings.TrimPrefix(tcd.ID, "call_")
					} else {
						itemID = "ws_" + uuid.New().String()
					}
					fcItemIDs[tcd.Index] = itemID
					fcEvent := converter.BuildStreamBuiltinToolItemAddedEvent(itemID, name+"_call", oidx)
					fmt.Fprint(c.Writer, fcEvent)
				} else {
					itemID := "fcall_" + uuid.New().String()
					fcItemIDs[tcd.Index] = itemID
					fcEvent := converter.BuildStreamFunctionCallItemAddedEvent(itemID, name, callID, oidx)
					fmt.Fprint(c.Writer, fcEvent)
				}
			}
			if tcd.Function.Arguments != "" && !fcIsBuiltin[tcd.Index] {
				oidx := fcOutputIndex[tcd.Index]
				itemID := fcItemIDs[tcd.Index]
				argEvent := converter.BuildStreamFunctionCallArgumentsDeltaEvent(itemID, tcd.Function.Arguments, oidx)
				fmt.Fprint(c.Writer, argEvent)
			}
			state.ToolCalls = append(state.ToolCalls, converter.StreamToolCall{
				Index:     tcd.Index,
				ID:        tcd.ID,
				Name:      tcd.Function.Name,
				Arguments: tcd.Function.Arguments,
			})
		}

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

				itemDoneEvent := converter.BuildStreamOutputItemDoneEvent(state.MsgID, state.TextOutputIndex, msgItem)
				fmt.Fprint(c.Writer, itemDoneEvent)
			}

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
				itemID := m.ItemID
				if itemID == "" {
					itemID = "fcall_" + uuid.New().String()
				}
				oidx := fcOutputIndex[m.Index]
				isBuiltin := fcIsBuiltin[m.Index]

				var fcItem map[string]interface{}
				if isBuiltin {
					var searchArgs struct {
						Query string `json:"query"`
					}
					json.Unmarshal([]byte(m.Arguments), &searchArgs)
					action := map[string]interface{}{
						"type": "search",
					}
					if searchArgs.Query != "" {
						action["query"] = searchArgs.Query
						action["queries"] = []string{searchArgs.Query}
					}
					fcItem = map[string]interface{}{
						"id":     itemID,
						"type":   m.Name + "_call",
						"status": "completed",
						"action": action,
					}
				} else {
					callID := m.ID
					if callID == "" {
						callID = "call_" + uuid.New().String()
					}
					fcItem = map[string]interface{}{
						"id":        itemID,
						"type":      "function_call",
						"call_id":   callID,
						"name":      m.Name,
						"arguments": m.Arguments,
						"status":    "completed",
					}
					argDone := converter.BuildStreamFunctionCallArgumentsDoneEvent(itemID, m.Arguments, oidx)
					fmt.Fprint(c.Writer, argDone)
				}

				output = append(output, fcItem)

				itemDoneEvent := converter.BuildStreamOutputItemDoneEvent(itemID, oidx, fcItem)
				fmt.Fprint(c.Writer, itemDoneEvent)
			}

			// 检查是否所有 tool calls 都是 web_search
			allWebSearch := len(mergeMap) > 0
			for _, m := range mergeMap {
				if !fcIsBuiltin[m.Index] {
					allWebSearch = false
					break
				}
			}

			if allWebSearch {
				// 取 query
				var searchQuery string
				var searchCallID string
				for _, m := range mergeMap {
					searchCallID = m.ID
					var args struct {
						Query string `json:"query"`
					}
					json.Unmarshal([]byte(m.Arguments), &args)
					if args.Query != "" {
						searchQuery = args.Query
						break
					}
				}

				if searchQuery != "" {
					if h.Searcher.Backend() == "searxng" {
						h.handleSearXNGStreamingSearch(c, state, searchQuery, searchCallID, &output, l, originalBody, authHeader)
					} else {
						var accumulatedText string
						totalAttempts := h.retryCount + 1
						gotAnswer := false

						for attempt := 0; attempt < totalAttempts; attempt++ {
							if attempt > 0 {
								l.Info("search: retrying", "query", searchQuery, "attempt", attempt+1, "max", totalAttempts)
							}

							results := h.Searcher.Search(c.Request.Context(), searchQuery)
							if len(results) == 0 {
								continue
							}

							resultsJSON, _ := json.Marshal(map[string]interface{}{
								"query":       searchQuery,
								"search_time": time.Now().Format("2006-01-02 15:04"),
								"results":     results,
							})
							l.Info("search: feeding results to model",
								"query", searchQuery,
								"result_count", len(results),
								"results_json", string(resultsJSON),
							)

							reInvokeBody, err := converter.BuildReInvokeRequest(originalBody, searchQuery, resultsJSON, searchCallID)
							if err != nil {
								continue
							}

							reInvokeResp, err := h.Proxy.Send(c.Request.Context(), c.Request.URL.Path, reInvokeBody, authHeader)
							if err != nil || reInvokeResp.StatusCode != http.StatusOK {
								if reInvokeResp != nil {
									reInvokeResp.Body.Close()
								}
								continue
							}

							reInvokeScanner := bufio.NewScanner(reInvokeResp.Body)
							reInvokeScanner.Buffer(make([]byte, 0, 1024*64), 1024*1024)

							accumulatedText = ""
							for reInvokeScanner.Scan() {
								rt, rFinish, _, _, _ := converter.ParseChatStreamLine(reInvokeScanner.Text())
								if rt != "" {
									accumulatedText += rt
								}
								if rFinish != "" {
									break
								}
							}
							reInvokeResp.Body.Close()

							l.Info("search: re-invoke response",
								"query", searchQuery,
								"attempt", attempt,
								"response_text", accumulatedText,
							)

							if accumulatedText != "" && !strings.HasPrefix(accumulatedText, "SEARCH_RESULT_INSUFFICIENT") {
								gotAnswer = true
								break
							}
							if strings.HasPrefix(accumulatedText, "SEARCH_RESULT_INSUFFICIENT") {
								l.Info("search: model reported insufficient results",
									"query", searchQuery, "attempt", attempt, "response", accumulatedText)
							}
						}

						if gotAnswer {
							msgID := "msg_" + uuid.New().String()
							msgOidx := state.NextOutputIndex()

							msgAddEvent := converter.BuildStreamItemAddedEvent(msgID, "assistant", msgOidx)
							fmt.Fprint(c.Writer, msgAddEvent)

							if accumulatedText != "" {
								deltaEvent := converter.BuildStreamTextDeltaEvent(msgID, accumulatedText, msgOidx, 0)
								fmt.Fprint(c.Writer, deltaEvent)
							}

							doneEvent := converter.BuildStreamTextDoneEvent(msgID, accumulatedText, msgOidx, 0)
							fmt.Fprint(c.Writer, doneEvent)

							contentArr := []map[string]interface{}{}
							if accumulatedText != "" {
								contentArr = append(contentArr, map[string]interface{}{
									"type":        "output_text",
									"text":        accumulatedText,
									"annotations": []interface{}{},
								})
							}
							msgItem := map[string]interface{}{
								"id":      msgID,
								"type":    "message",
								"role":    "assistant",
								"content": contentArr,
							}
							msgDoneEvent := converter.BuildStreamOutputItemDoneEvent(msgID, msgOidx, msgItem)
							fmt.Fprint(c.Writer, msgDoneEvent)
							output = append(output, msgItem)
						} else {
							fallbackText := "当前未搜索到相关信息，请调整查询词后重试。"
							l.Warn("search: all retries exhausted, using fallback",
								"query", searchQuery,
								"fallback", fallbackText,
							)
							output = append(output, appendFallbackMessage(c, state, fallbackText)...)
						}
					}
				} else {
					l.Warn("search: empty query, using fallback",
						"query", searchQuery,
					)
					fallbackText := "当前未搜索到相关信息，请调整查询词后重试。"
					output = append(output, appendFallbackMessage(c, state, fallbackText)...)
				}
			}

			l.Info("sending response.completed",
				"resp_id", state.RespID,
				"model", state.Model,
				"status", status,
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
		"resp_id", state.RespID,
		"model", state.Model,
		"status", http.StatusOK,
		"duration", time.Since(start).String(),
		"type", "stream",
		"text_length", len(state.AccumulatedText),
		"has_text", state.HasContent,
		"tool_call_count", len(state.ToolCalls),
		"usage", fmt.Sprintf(`{"input_tokens":%d,"output_tokens":%d}`, state.PromptTokens, state.CompletionTokens),
	)
}

func appendFallbackMessage(c *gin.Context, state *converter.StreamState, text string) []map[string]interface{} {
	msgID := "msg_" + uuid.New().String()
	oidx := state.NextOutputIndex()

	msgAddEvent := converter.BuildStreamItemAddedEvent(msgID, "assistant", oidx)
	fmt.Fprint(c.Writer, msgAddEvent)

	deltaEvent := converter.BuildStreamTextDeltaEvent(msgID, text, oidx, 0)
	fmt.Fprint(c.Writer, deltaEvent)

	doneEvent := converter.BuildStreamTextDoneEvent(msgID, text, oidx, 0)
	fmt.Fprint(c.Writer, doneEvent)

	contentArr := []map[string]interface{}{
		{"type": "output_text", "text": text, "annotations": []interface{}{}},
	}
	msgItem := map[string]interface{}{
		"id":      msgID,
		"type":    "message",
		"role":    "assistant",
		"content": contentArr,
	}

	msgDoneEvent := converter.BuildStreamOutputItemDoneEvent(msgID, oidx, msgItem)
	fmt.Fprint(c.Writer, msgDoneEvent)

	return []map[string]interface{}{msgItem}
}

func extractWebSearchToolCall(rawBody []byte) (toolCallID, query string) {
	var chatResp struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				ToolCalls json.RawMessage `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rawBody, &chatResp); err != nil {
		return "", ""
	}
	if len(chatResp.Choices) == 0 {
		return "", ""
	}

	var toolCalls []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(chatResp.Choices[0].Message.ToolCalls, &toolCalls); err != nil {
		return "", ""
	}
	for _, tc := range toolCalls {
		if converter.IsBuiltinTool(tc.Function.Name) {
			var args struct {
				Query string `json:"query"`
			}
			json.Unmarshal([]byte(tc.Function.Arguments), &args)
			return tc.ID, args.Query
		}
	}
	return "", ""
}

func extractContentFromResponse(rawBody []byte) string {
	var chatResp struct {
		Choices []struct {
			Message struct {
				Content *string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rawBody, &chatResp); err != nil {
		return ""
	}
	if len(chatResp.Choices) > 0 && chatResp.Choices[0].Message.Content != nil {
		return *chatResp.Choices[0].Message.Content
	}
	return ""
}

func buildSearchResponse(originalRespBody []byte, msgText string) []byte {
	var chatResp struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				ToolCalls json.RawMessage `json:"tool_calls"`
			} `json:"message"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage,omitempty"`
	}
	json.Unmarshal(originalRespBody, &chatResp)

	var output []map[string]interface{}

	if len(chatResp.Choices) > 0 && chatResp.Choices[0].Message.ToolCalls != nil {
		var toolCalls []struct {
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		}
		json.Unmarshal(chatResp.Choices[0].Message.ToolCalls, &toolCalls)
		for _, tc := range toolCalls {
			if converter.IsBuiltinTool(tc.Function.Name) {
				var args struct {
					Query string `json:"query"`
				}
				json.Unmarshal([]byte(tc.Function.Arguments), &args)
				action := map[string]interface{}{
					"type": "search",
				}
				if args.Query != "" {
					action["query"] = args.Query
					action["queries"] = []string{args.Query}
				}
				output = append(output, map[string]interface{}{
					"id":     "ws_" + uuid.New().String(),
					"type":   tc.Function.Name + "_call",
					"status": "completed",
					"action": action,
				})
			}
		}
	}

	output = append(output, map[string]interface{}{
		"id":   "msg_" + uuid.New().String(),
		"type": "message",
		"role": "assistant",
		"content": []map[string]interface{}{
			{"type": "output_text", "text": msgText, "annotations": []interface{}{}},
		},
	})

	inputTokens := 0
	outputTokens := 0
	if chatResp.Usage != nil {
		inputTokens = chatResp.Usage.PromptTokens
		outputTokens = chatResp.Usage.CompletionTokens
	}

	resp := map[string]interface{}{
		"id":         "resp_" + uuid.New().String(),
		"object":     "response",
		"created_at": time.Now().Unix(),
		"model":      chatResp.Model,
		"status":     "completed",
		"output":     output,
		"usage": map[string]interface{}{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
			"total_tokens":  inputTokens + outputTokens,
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

func (h *ResponsesHandler) handleSearXNGNonStreaming(c *gin.Context, rawBody []byte, query, callID string, start time.Time, l *slog.Logger, originalBody []byte, authHeader string) {
	results := h.Searcher.Search(c.Request.Context(), query)
	resultsText := formatSearchResults(results)

	if h.searxngSummarize && len(results) > 0 {
		resultsJSON, _ := json.Marshal(results)
		reInvokeBody, err := converter.BuildReInvokeRequest(originalBody, query, resultsJSON, callID)
		if err == nil {
			var m map[string]interface{}
			json.Unmarshal(reInvokeBody, &m)
			m["stream"] = false
			reInvokeBody, _ = json.Marshal(m)

			reInvokeResp, err := h.Proxy.Send(c.Request.Context(), c.Request.URL.Path, reInvokeBody, authHeader)
			if err == nil && reInvokeResp.StatusCode == http.StatusOK {
				reInvokeRaw, _ := io.ReadAll(reInvokeResp.Body)
				reInvokeResp.Body.Close()
				if modelText := extractContentFromResponse(reInvokeRaw); modelText != "" {
					l.Info("search: searxng summarize result", "query", query, "summary", modelText)
					resultsText = modelText
				}
			}
		}
	}

	if len(results) > 0 {
		resultsJSON, _ := json.Marshal(map[string]interface{}{
			"query":       query,
			"search_time": time.Now().Format("2006-01-02 15:04"),
			"results":     results,
		})
		l.Info("search: searxng results", "query", query, "result_count", len(results), "results_json", string(resultsJSON))
	}

	converted := buildSearXNGSearchResponse(rawBody, query, callID, resultsText)
	var respMeta struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	}
	json.Unmarshal(converted, &respMeta)
	l.Info("outgoing response",
		"resp_id", respMeta.ID,
		"model", respMeta.Model,
		"status", http.StatusOK,
		"duration", time.Since(start).String(),
		"body", string(converted),
	)
	c.Data(http.StatusOK, "application/json", converted)
}

func (h *ResponsesHandler) handleSearXNGStreamingSearch(c *gin.Context, state *converter.StreamState, searchQuery, searchCallID string, output *[]map[string]interface{}, l *slog.Logger, originalBody []byte, authHeader string) {
	results := h.Searcher.Search(c.Request.Context(), searchQuery)
	resultsText := formatSearchResults(results)

	if h.searxngSummarize && len(results) > 0 {
		resultsJSON, _ := json.Marshal(results)
		reInvokeBody, err := converter.BuildReInvokeRequest(originalBody, searchQuery, resultsJSON, searchCallID)
		if err == nil {
			var m map[string]interface{}
			json.Unmarshal(reInvokeBody, &m)
			m["stream"] = false
			reInvokeBody, _ = json.Marshal(m)

			reInvokeResp, err := h.Proxy.Send(c.Request.Context(), c.Request.URL.Path, reInvokeBody, authHeader)
			if err == nil && reInvokeResp.StatusCode == http.StatusOK {
				reInvokeRaw, _ := io.ReadAll(reInvokeResp.Body)
				reInvokeResp.Body.Close()
				if modelText := extractContentFromResponse(reInvokeRaw); modelText != "" {
					l.Info("search: searxng summarize result", "query", searchQuery, "summary", modelText)
					resultsText = modelText
				}
			}
		}
	}

	if len(results) > 0 {
		resultsJSON, _ := json.Marshal(map[string]interface{}{
			"query":       searchQuery,
			"search_time": time.Now().Format("2006-01-02 15:04"),
			"results":     results,
		})
		l.Info("search: searxng results", "query", searchQuery, "result_count", len(results), "results_json", string(resultsJSON))
	}

	msgID := "msg_" + uuid.New().String()
	msgOidx := state.NextOutputIndex()

	state.AccumulatedText = resultsText
	state.HasContent = resultsText != ""

	msgAddEvent := converter.BuildStreamItemAddedEvent(msgID, "assistant", msgOidx)
	fmt.Fprint(c.Writer, msgAddEvent)

	if resultsText != "" {
		deltaEvent := converter.BuildStreamTextDeltaEvent(msgID, resultsText, msgOidx, 0)
		fmt.Fprint(c.Writer, deltaEvent)
	}

	doneEvent := converter.BuildStreamTextDoneEvent(msgID, resultsText, msgOidx, 0)
	fmt.Fprint(c.Writer, doneEvent)

	msgItem := map[string]interface{}{
		"id":   msgID,
		"type": "message",
		"role": "assistant",
		"content": []map[string]interface{}{
			{"type": "output_text", "text": resultsText, "annotations": []interface{}{}},
		},
	}
	msgDoneEvent := converter.BuildStreamOutputItemDoneEvent(msgID, msgOidx, msgItem)
	fmt.Fprint(c.Writer, msgDoneEvent)
	*output = append(*output, msgItem)
}

func buildSearXNGSearchResponse(originalRespBody []byte, query, callID, text string) []byte {
	var chatResp struct {
		Model string `json:"model"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage,omitempty"`
	}
	json.Unmarshal(originalRespBody, &chatResp)

	wsID := "ws_" + strings.TrimPrefix(callID, "call_")

	output := []map[string]interface{}{
		{
			"id":     wsID,
			"type":   "web_search_call",
			"status": "completed",
			"action": map[string]interface{}{
				"type":    "search",
				"query":   query,
				"queries": []string{query},
			},
		},
		{
			"id":   "msg_" + uuid.New().String(),
			"type": "message",
			"role": "assistant",
			"content": []map[string]interface{}{
				{"type": "output_text", "text": text, "annotations": []interface{}{}},
			},
		},
	}

	inputTokens := 0
	outputTokens := 0
	if chatResp.Usage != nil {
		inputTokens = chatResp.Usage.PromptTokens
		outputTokens = chatResp.Usage.CompletionTokens
	}

	resp := map[string]interface{}{
		"id":         "resp_" + uuid.New().String(),
		"object":     "response",
		"created_at": time.Now().Unix(),
		"model":      chatResp.Model,
		"status":     "completed",
		"output":     output,
		"usage": map[string]interface{}{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
			"total_tokens":  inputTokens + outputTokens,
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

func formatSearchResults(results []searcher.SearchResult) string {
	if len(results) == 0 {
		return ""
	}
	var b strings.Builder
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "%d. %s\n%s\n%s", i+1, r.Title, r.URL, r.PageContent)
	}
	return b.String()
}
