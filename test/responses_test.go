package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

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
	s := searcher.New(0, 0, "", 0)
	h := handler.NewResponsesHandler(p, s, 3)

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
	h := handler.NewResponsesHandler(p, searcher.New(0, 0, "", 0), 3)

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
	h := handler.NewResponsesHandler(p, searcher.New(0, 0, "", 0), 3)

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
	h := handler.NewResponsesHandler(p, searcher.New(0, 0, "", 0), 3)

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
