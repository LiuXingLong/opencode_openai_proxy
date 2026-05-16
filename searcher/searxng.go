package searcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

func (s *Searcher) searchSearXNG(ctx context.Context, query string) ([]SearchResult, error) {
	reqURL := fmt.Sprintf("%s/search?q=%s&format=json&language=zh-CN", s.searxngURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create searxng request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("searxng request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng returned status %d", resp.StatusCode)
	}

	var searxngResp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&searxngResp); err != nil {
		return nil, fmt.Errorf("decode searxng response: %w", err)
	}

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
