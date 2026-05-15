package searcher

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestSearchBeijingWeather(t *testing.T) {
	s := New(10, 30*time.Second, "https://www.bing.com/search?q=", 5)
	results := s.Search(context.Background(), "今天北京天气如何")

	if len(results) == 0 {
		t.Fatal("搜索未返回结果")
	}

	t.Logf("共 %d 条结果\n", len(results))
	for i, r := range results {
		t.Logf("\n========== [%d] ==========", i)
		t.Logf("  标题: %s", r.Title)
		t.Logf("  URL: %s", r.URL)
		t.Logf("  摘要: %s", truncate(r.Snippet, 200))
		t.Logf("  页面内容长度: %d 字符", len(r.PageContent))
		if r.PageContent != "" {
			t.Logf("  页面内容(前500): %s", truncate(r.PageContent, 500))
		}
	}

	// 输出完整 JSON 格式（发给模型的格式）
	resultsJSON, _ := json.Marshal(map[string]interface{}{
		"query":   "今天北京天气如何",
		"results": results,
	})
	fmt.Printf("\n%s\n", string(resultsJSON))
}

func TestSearchFinalPrompt(t *testing.T) {
	s := New(10, 30*time.Second, "https://www.bing.com/search?q=", 5)
	results := s.Search(context.Background(), "今天北京天气如何")

	if len(results) == 0 {
		t.Fatal("搜索未返回结果")
	}

	resultsJSON, _ := json.Marshal(map[string]interface{}{
		"query":   "今天北京天气如何",
		"results": results,
	})

	wrapper := fmt.Sprintf(`请仅根据以下搜索结果回答用户的问题，不要依赖你自身的知识。
如果搜索结果中不包含足够的信息来回答问题，请仅回复：
SEARCH_RESULT_INSUFFICIENT: 简短说明原因

搜索结果（JSON格式）：
%s`, string(resultsJSON))

	fmt.Printf("\n========== 最终发给模型的 prompt ==========\n%s\n", wrapper)
	fmt.Printf("\n========== JSON 字节数: %d ==========\n", len(resultsJSON))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
