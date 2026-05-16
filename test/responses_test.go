package test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LiuXingLong/opencode-openai-proxy/handler"
	"github.com/LiuXingLong/opencode-openai-proxy/middleware"
	"github.com/LiuXingLong/opencode-openai-proxy/proxy"
	"github.com/LiuXingLong/opencode-openai-proxy/searcher"
)

func chatResponse(id, content string) string {
	b, _ := json.Marshal(map[string]interface{}{
		"id":      id,
		"object":  "chat.completion",
		"created": 1715000000,
		"model":   "test-model",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	})
	return string(b)
}

func requestBody(model, input string) string {
	b, _ := json.Marshal(map[string]interface{}{
		"model":  model,
		"input":  input,
		"stream": false,
	})
	return string(b)
}

func extractText(resp map[string]interface{}) string {
	output, _ := resp["output"].([]interface{})
	if len(output) == 0 {
		return ""
	}
	msg, _ := output[0].(map[string]interface{})
	content, _ := msg["content"].([]interface{})
	if len(content) == 0 {
		return ""
	}
	part, _ := content[0].(map[string]interface{})
	text, _ := part["text"].(string)
	return text
}

func TestPathRoutingDefaultUpstream(t *testing.T) {
	defaultUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, chatResponse("chatcmpl-default", "来自默认上游"))
	}))
	defer defaultUpstream.Close()

	ollamaUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, chatResponse("chatcmpl-ollama", "来自 ollama"))
	}))
	defer ollamaUpstream.Close()

	routeMap := map[string]string{
		"/ollama/v1/responses": ollamaUpstream.URL,
	}

	p := proxy.New(defaultUpstream.URL, routeMap)
	s := searcher.New(0, 0, "", 0, "", "")
	h := handler.NewResponsesHandler(p, s, 3, false)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Trace())
	r.Use(middleware.Auth())

	registered := map[string]bool{"/v1/responses": true}
	for path := range routeMap {
		registered[path] = true
	}
	for path := range registered {
		r.POST(path, h.Create)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/responses", bytes.NewReader([]byte(requestBody("big-pickle", "你好"))))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/v1/responses: expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if got := extractText(resp); got != "来自默认上游" {
		t.Errorf("default upstream: expected '来自默认上游', got %q", got)
	}
}

func TestPathRoutingOllamaUpstream(t *testing.T) {
	defaultUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, chatResponse("chatcmpl-default", "来自默认上游"))
	}))
	defer defaultUpstream.Close()

	ollamaUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, chatResponse("chatcmpl-ollama", "来自 ollama"))
	}))
	defer ollamaUpstream.Close()

	routeMap := map[string]string{
		"/ollama/v1/responses": ollamaUpstream.URL,
	}

	p := proxy.New(defaultUpstream.URL, routeMap)
	h := handler.NewResponsesHandler(p, searcher.New(0, 0, "", 0, "", ""), 3, false)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Trace())
	r.Use(middleware.Auth())

	registered := map[string]bool{"/v1/responses": true}
	for path := range routeMap {
		registered[path] = true
	}
	for path := range registered {
		r.POST(path, h.Create)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/ollama/v1/responses", bytes.NewReader([]byte(requestBody("gpt-oss:20b", "你好"))))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/ollama/v1/responses: expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if got := extractText(resp); got != "来自 ollama" {
		t.Errorf("ollama upstream: expected '来自 ollama', got %q", got)
	}
}

func TestPathRoutingOllamaUpstreamWrongPath(t *testing.T) {
	defaultUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, chatResponse("chatcmpl-default", "来自默认上游"))
	}))
	defer defaultUpstream.Close()

	ollamaUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, chatResponse("chatcmpl-ollama", "来自 ollama"))
	}))
	defer ollamaUpstream.Close()

	routeMap := map[string]string{
		"/ollama/v1/responses": ollamaUpstream.URL,
	}

	p := proxy.New(defaultUpstream.URL, routeMap)
	h := handler.NewResponsesHandler(p, searcher.New(0, 0, "", 0, "", ""), 3, false)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Trace())
	r.Use(middleware.Auth())

	registered := map[string]bool{"/v1/responses": true}
	for path := range routeMap {
		registered[path] = true
	}
	for path := range registered {
		r.POST(path, h.Create)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/responses", bytes.NewReader([]byte(requestBody("gpt-oss:20b", "你好"))))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/v1/responses: expected 200, got %d", w.Code)
	}
}

func TestPathRoutingNotFound(t *testing.T) {
	defaultUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, chatResponse("chatcmpl-default", "来自默认上游"))
	}))
	defer defaultUpstream.Close()

	routeMap := map[string]string{
		"/ollama/v1/responses": "http://127.0.0.1:11434",
	}

	p := proxy.New(defaultUpstream.URL, routeMap)
	h := handler.NewResponsesHandler(p, searcher.New(0, 0, "", 0, "", ""), 3, false)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Trace())
	r.Use(middleware.Auth())

	registered := map[string]bool{"/v1/responses": true}
	for path := range routeMap {
		registered[path] = true
	}
	for path := range registered {
		r.POST(path, h.Create)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/unknown/path", bytes.NewReader([]byte(requestBody("test", "你好"))))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("unregistered path: expected 404, got %d", w.Code)
	}
}

func toolCallResponse(callID, name, args string) string {
	b, _ := json.Marshal(map[string]interface{}{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"created": 1715000000,
		"model":   "test-model",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role": "assistant",
					"tool_calls": []map[string]interface{}{
						{
							"id":   callID,
							"type": "function",
							"function": map[string]interface{}{
								"name":      name,
								"arguments": args,
							},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	})
	return string(b)
}

func TestSearXNGNonStreaming(t *testing.T) {
	searxngSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "test query" {
			t.Errorf("unexpected query: %s", r.URL.Query().Get("q"))
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{"title": "R1", "url": "https://e.com/1", "content": "content 1"},
			},
		})
	}))
	defer searxngSrv.Close()

	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, toolCallResponse("call_testcallid", "web_search", `{"query":"test query"}`))
	}))
	defer upstreamSrv.Close()

	p := proxy.New(upstreamSrv.URL, nil)
	s := searcher.New(0, 10*time.Second, "", 0, "searxng", searxngSrv.URL)
	h := handler.NewResponsesHandler(p, s, 3, false)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Trace())
	r.Use(middleware.Auth())
	r.POST("/v1/responses", h.Create)

	body := `{"model":"test-model","input":"test query","stream":false}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/responses", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	output, _ := resp["output"].([]interface{})
	if len(output) != 2 {
		t.Fatalf("expected 2 output items, got %d", len(output))
	}

	wsItem, _ := output[0].(map[string]interface{})
	if wsItem["type"] != "web_search_call" {
		t.Errorf("expected type web_search_call, got %q", wsItem["type"])
	}
	if wsItem["id"] != "ws_testcallid" {
		t.Errorf("expected id ws_testcallid, got %q", wsItem["id"])
	}
	action, _ := wsItem["action"].(map[string]interface{})
	if action["query"] != "test query" {
		t.Errorf("expected query 'test query', got %q", action["query"])
	}

	msgItem, _ := output[1].(map[string]interface{})
	if msgItem["type"] != "message" {
		t.Errorf("expected type message, got %q", msgItem["type"])
	}
	content, _ := msgItem["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(content))
	}
	part, _ := content[0].(map[string]interface{})
	text, _ := part["text"].(string)
	expectedText := "1. R1\nhttps://e.com/1\ncontent 1"
	if text != expectedText {
		t.Errorf("expected text %q, got %q", expectedText, text)
	}
}

func TestSearXNGStreaming(t *testing.T) {
	searxngSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{"title": "R1", "url": "https://e.com/1", "content": "content 1"},
			},
		})
	}))
	defer searxngSrv.Close()

	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_testcallid","type":"function","function":{"name":"web_search","arguments":""}}]}}]}`)
		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"query\": \"test query\"}"}}]}}]}`)
		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer upstreamSrv.Close()

	p := proxy.New(upstreamSrv.URL, nil)
	s := searcher.New(0, 10*time.Second, "", 0, "searxng", searxngSrv.URL)
	h := handler.NewResponsesHandler(p, s, 3, false)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Trace())
	r.Use(middleware.Auth())
	r.POST("/v1/responses", h.Create)

	body := `{"model":"test-model","input":"test query","stream":true}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/responses", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var completedResponse map[string]interface{}
	scanner := bufio.NewScanner(bytes.NewReader(w.Body.Bytes()))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		dataStr := strings.TrimPrefix(line, "data: ")
		var event struct {
			Type     string                 `json:"type"`
			Response map[string]interface{} `json:"response"`
		}
		if err := json.Unmarshal([]byte(dataStr), &event); err != nil {
			continue
		}
		if event.Type == "response.completed" {
			completedResponse = event.Response
		}
	}

	if completedResponse == nil {
		t.Fatal("no response.completed event found")
	}

	output, _ := completedResponse["output"].([]interface{})
	if len(output) != 2 {
		t.Fatalf("expected 2 output items, got %d", len(output))
	}

	wsItem, _ := output[0].(map[string]interface{})
	if wsItem["type"] != "web_search_call" {
		t.Errorf("expected type web_search_call, got %q", wsItem["type"])
	}
	if wsItem["id"] != "ws_testcallid" {
		t.Errorf("expected id ws_testcallid, got %q", wsItem["id"])
	}

	msgItem, _ := output[1].(map[string]interface{})
	if msgItem["type"] != "message" {
		t.Errorf("expected type message, got %q", msgItem["type"])
	}
	content, _ := msgItem["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(content))
	}
	part, _ := content[0].(map[string]interface{})
	text, _ := part["text"].(string)
	expectedText := "1. R1\nhttps://e.com/1\ncontent 1"
	if text != expectedText {
		t.Errorf("expected text %q, got %q", expectedText, text)
	}
}

func TestSearXNGNonStreamingWithSummarize(t *testing.T) {
	searxngSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{"title": "Result 1", "url": "https://e.com/1", "content": "content one"},
				{"title": "Result 2", "url": "https://e.com/2", "content": "content two"},
			},
		})
	}))
	defer searxngSrv.Close()

	reInvokeCalled := false
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !reInvokeCalled {
			reInvokeCalled = true
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, toolCallResponse("call_reinvoke", "web_search", `{"query":"test query"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, chatResponse("chatcmpl-summary", "这是模型根据搜索结果生成的总结"))
	}))
	defer upstreamSrv.Close()

	p := proxy.New(upstreamSrv.URL, nil)
	s := searcher.New(10, 10*time.Second, "", 0, "searxng", searxngSrv.URL)
	h := handler.NewResponsesHandler(p, s, 3, true)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Trace())
	r.Use(middleware.Auth())
	r.POST("/v1/responses", h.Create)

	body := `{"model":"test-model","input":"test query","stream":false}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/responses", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	output, _ := resp["output"].([]interface{})
	if len(output) != 2 {
		t.Fatalf("expected 2 output items, got %d", len(output))
	}
	msgItem, _ := output[1].(map[string]interface{})
	content, _ := msgItem["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(content))
	}
	part, _ := content[0].(map[string]interface{})
	text, _ := part["text"].(string)
	expected := "这是模型根据搜索结果生成的总结"
	if text != expected {
		t.Errorf("expected summary %q, got %q", expected, text)
	}
}

func TestSearXNGStreamingWithSummarize(t *testing.T) {
	searxngSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{"title": "Result 1", "url": "https://e.com/1", "content": "content one"},
			},
		})
	}))
	defer searxngSrv.Close()

	reInvokeCount := 0
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reInvokeCount == 0 {
			reInvokeCount++
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_testcallid","type":"function","function":{"name":"web_search","arguments":""}}]}}]}`)
			fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"query\": \"test query\"}"}}]}}]}`)
			fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
			fmt.Fprintf(w, "data: [DONE]\n\n")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, chatResponse("chatcmpl-summary", "流式模型总结结果"))
	}))
	defer upstreamSrv.Close()

	p := proxy.New(upstreamSrv.URL, nil)
	s := searcher.New(10, 10*time.Second, "", 0, "searxng", searxngSrv.URL)
	h := handler.NewResponsesHandler(p, s, 3, true)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Trace())
	r.Use(middleware.Auth())
	r.POST("/v1/responses", h.Create)

	body := `{"model":"test-model","input":"test query","stream":true}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/responses", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var completedResponse map[string]interface{}
	scanner := bufio.NewScanner(bytes.NewReader(w.Body.Bytes()))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		dataStr := strings.TrimPrefix(line, "data: ")
		var event struct {
			Type     string                 `json:"type"`
			Response map[string]interface{} `json:"response"`
		}
		if err := json.Unmarshal([]byte(dataStr), &event); err != nil {
			continue
		}
		if event.Type == "response.completed" {
			completedResponse = event.Response
		}
	}

	if completedResponse == nil {
		t.Fatal("no response.completed event found")
	}

	output, _ := completedResponse["output"].([]interface{})
	if len(output) != 2 {
		t.Fatalf("expected 2 output items, got %d", len(output))
	}

	msgItem, _ := output[1].(map[string]interface{})
	content, _ := msgItem["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(content))
	}
	part, _ := content[0].(map[string]interface{})
	text, _ := part["text"].(string)
	expected := "流式模型总结结果"
	if text != expected {
		t.Errorf("expected summary %q, got %q", expected, text)
	}
}
