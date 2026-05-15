package searcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
)

func (s *Searcher) fetchPages(ctx context.Context, results []SearchResult) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, s.concurrency)

	for i, r := range results {
		if r.URL == "" || r.PageContent != "" {
			continue
		}

		wg.Add(1)
		sem <- struct{}{}

		go func(idx int, pageURL string) {
			defer wg.Done()
			defer func() { <-sem }()

			content, err := s.fetchPage(ctx, pageURL)
			if err != nil || content == "" {
				return
			}
			results[idx].PageContent = content
		}(i, r.URL)
	}

	wg.Wait()
}

func (s *Searcher) fetchPage(ctx context.Context, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("create page request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := s.pageClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("page request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("page returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read page body: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("parse page html: %w", err)
	}

	doc.Find("script, style, nav, footer, header, aside, noscript, iframe, svg, form, button, input, select, textarea, path, meta, link").Remove()

	extractText := func(sel string) string {
		s := strings.TrimSpace(doc.Find(sel).Text())
		return strings.Join(strings.Fields(s), " ")
	}

	var contentParts []string
	selectors := []string{
		"#gs_main",
		".slide.wptSld.rowSpan4.colSpan5",
		".b_gwaDlWrapper",
		"main",
		"article",
		"[role=main], .content, .article-content, #content, .post-content",
	}
	for _, sel := range selectors {
		if t := extractText(sel); t != "" {
			contentParts = append(contentParts, t)
		}
	}

	content := strings.Join(contentParts, "\n\n")
	if len(content) < 5000 {
		if t := extractText("body"); t != "" {
			content = t
		}
	}

	if len(content) > 5000 {
		content = content[:5000]
	}

	return content, nil
}
