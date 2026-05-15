package searcher

import (
	"context"
	"net/http"
	"time"
)

type SearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Snippet     string `json:"snippet"`
	PageContent string `json:"page_content"`
}

type Searcher struct {
	client      *http.Client
	pageClient  *http.Client
	resultCount int
	timeout     time.Duration
	bingURL     string
	concurrency int
}

func New(resultCount int, timeout time.Duration, bingURL string, concurrency int) *Searcher {
	if concurrency <= 0 {
		concurrency = resultCount
	}
	return &Searcher{
		client:      &http.Client{Timeout: 10 * time.Second},
		pageClient:  &http.Client{Timeout: 15 * time.Second},
		resultCount: resultCount,
		timeout:     timeout,
		bingURL:     bingURL,
		concurrency: concurrency,
	}
}

func (s *Searcher) Search(ctx context.Context, query string) []SearchResult {
	searchCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	results, err := s.searchBing(searchCtx, query)
	if err != nil || len(results) == 0 {
		return nil
	}

	if s.resultCount > 0 && len(results) > s.resultCount {
		results = results[:s.resultCount]
	}

	s.fetchPages(searchCtx, results)

	filtered := results[:0]
	for _, r := range results {
		if r.PageContent != "" {
			filtered = append(filtered, r)
		}
	}

	return filtered
}
