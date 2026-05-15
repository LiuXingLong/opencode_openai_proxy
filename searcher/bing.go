package searcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func (s *Searcher) searchBing(ctx context.Context, query string) ([]SearchResult, error) {
	dateStr := time.Now().Format("2006-01-02")
	reqURL := fmt.Sprintf("%s%s", s.bingURL, url.QueryEscape(query+" "+dateStr))
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create bing request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bing request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("bing returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read bing response: %w", err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parse bing html: %w", err)
	}

	// 提取 SERP 页面正文（去噪音后作为第一个结果）
	var serpText string
	serpDoc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err == nil {
		serpDoc.Find("script, style, nav, footer, header, aside, noscript, iframe, svg, form, button, input, select, textarea, path, meta, link").Remove()
		serpText = strings.TrimSpace(serpDoc.Find("body").Text())
		serpText = strings.Join(strings.Fields(serpText), " ")
		if len(serpText) > 5000 {
			serpText = serpText[:5000]
		}
	}

	// 从 #b_results 提取 URL 并去重
	seen := map[string]bool{}
	type urlItem struct {
		title   string
		url     string
		snippet string
	}
	var urlItems []urlItem

	doc.Find("#b_results .b_algo").Each(func(i int, sel *goquery.Selection) {
		titleEl := sel.Find("h2 a")
		title := strings.TrimSpace(titleEl.Text())
		href, _ := titleEl.Attr("href")
		snippet := strings.TrimSpace(sel.Find(".b_caption p").Text())
		cooked := cleanURL(href)
		if title == "" || cooked == "" || seen[cooked] {
			return
		}
		seen[cooked] = true
		urlItems = append(urlItems, urlItem{title: title, url: cooked, snippet: snippet})
	})

	var results []SearchResult
	if serpText != "" {
		results = append(results, SearchResult{
			Title:       "Bing 搜索结果页",
			URL:         reqURL,
			PageContent: serpText,
		})
	}
	for _, u := range urlItems {
		results = append(results, SearchResult{
			Title:   u.title,
			URL:     u.url,
			Snippet: u.snippet,
		})
	}

	return results, nil
}

func cleanURL(raw string) string {
	if raw == "" {
		return ""
	}

	if !strings.HasPrefix(raw, "http") {
		if strings.Contains(raw, "bing.com/url?") {
			parsed, err := url.Parse(raw)
			if err == nil {
				if q := parsed.Query().Get("q"); q != "" {
					decoded, err := url.QueryUnescape(q)
					if err == nil {
						return decoded
					}
					return q
				}
			}
		}
		return ""
	}

	return raw
}
