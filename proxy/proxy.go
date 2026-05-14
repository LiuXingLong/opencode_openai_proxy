package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/LiuXingLong/opencode-openai-proxy/logger"
)

type Proxy struct {
	defaultBaseURL string
	routeMap       map[string]string
	client         *http.Client
}

func New(defaultBaseURL string, routeMap map[string]string) *Proxy {
	return &Proxy{
		defaultBaseURL: defaultBaseURL,
		routeMap:       routeMap,
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (p *Proxy) selectBaseURL(path string) string {
	var matchedURL string
	var matchedLen int
	for prefix, url := range p.routeMap {
		if strings.HasPrefix(path, prefix) && len(prefix) > matchedLen {
			matchedURL = url
			matchedLen = len(prefix)
		}
	}
	if matchedURL == "" {
		return p.defaultBaseURL
	}
	return matchedURL
}

func (p *Proxy) Send(ctx context.Context, path string, body []byte, authHeader string) (*http.Response, error) {
	baseURL := p.selectBaseURL(path)
	upstreamURL := baseURL + "/v1/chat/completions"
	l := logger.FromContext(ctx)

	l.Info("upstream request",
		"method", "POST",
		"url", upstreamURL,
		"body", string(body),
	)

	req, err := http.NewRequestWithContext(ctx, "POST", upstreamURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHeader)

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}

	l.Info("upstream response",
		"status", resp.StatusCode,
		"duration", time.Since(start).String(),
	)

	return resp, nil
}

func ReadBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
