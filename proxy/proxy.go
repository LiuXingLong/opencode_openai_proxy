package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/LiuXingLong/opencode-openai-proxy/logger"
)

type Proxy struct {
	baseURL string
	client  *http.Client
}

func New(baseURL string) *Proxy {
	return &Proxy{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (p *Proxy) Send(ctx context.Context, body []byte, authHeader string) (*http.Response, error) {
	upstreamURL := p.baseURL + "/v1/chat/completions"
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
