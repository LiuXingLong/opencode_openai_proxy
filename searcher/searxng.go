package searcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/LiuXingLong/opencode-openai-proxy/logger"
)

func (s *Searcher) searchSearXNG(ctx context.Context, query string) ([]SearchResult, error) {
	l := logger.FromContext(ctx)

	reqURL := fmt.Sprintf("%s/search?q=%s&format=json&language=zh-CN", s.searxngURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create searxng request: %w", err)
	}

	l.Info("searxng: sending request", "query", query, "url", reqURL)

	resp, err := s.client.Do(req)
	if err != nil {
		l.Error("searxng: request failed", "query", query, "error", err.Error())
		return nil, fmt.Errorf("searxng request failed: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read searxng response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		l.Error("searxng: http error",
			"query", query,
			"status", resp.StatusCode,
			"body", string(rawBody),
		)
		return nil, fmt.Errorf("searxng returned status %d: %s", resp.StatusCode, string(rawBody))
	}

	var searxngResp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rawBody, &searxngResp); err != nil {
		l.Error("searxng: decode error", "query", query, "error", err.Error(), "body", string(rawBody))
		return nil, fmt.Errorf("decode searxng response: %w", err)
	}

	l.Info("searxng: response",
		"query", query,
		"result_count", len(searxngResp.Results),
		"body", string(rawBody),
	)

	var results []SearchResult
	for _, r := range searxngResp.Results {
		results = append(results, SearchResult{
			Title:       r.Title,
			URL:         r.URL,
			Snippet:     r.Content,
			PageContent: r.Content,
		})
	}
	return results, nil
}
