package searcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSearXNGSearchSuccess(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "test query" {
			t.Errorf("unexpected query: %s", r.URL.Query().Get("q"))
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{"title": "Result 1", "url": "https://example.com/1", "content": "Content one"},
				{"title": "Result 2", "url": "https://example.com/2", "content": "Content two"},
			},
		})
	}))
	defer mockSrv.Close()

	s := New(10, 10*time.Second, "", 0, "searxng", mockSrv.URL)
	results := s.Search(context.Background(), "test query")

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "Result 1" {
		t.Errorf("expected title 'Result 1', got %q", results[0].Title)
	}
	if results[0].PageContent != "Content one" {
		t.Errorf("expected page_content 'Content one', got %q", results[0].PageContent)
	}
	if results[0].Snippet != "Content one" {
		t.Errorf("expected snippet 'Content one', got %q", results[0].Snippet)
	}
	if results[0].URL != "https://example.com/1" {
		t.Errorf("expected URL 'https://example.com/1', got %q", results[0].URL)
	}
}

func TestSearXNGSearchEmptyResults(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}})
	}))
	defer mockSrv.Close()

	s := New(10, 10*time.Second, "", 0, "searxng", mockSrv.URL)
	results := s.Search(context.Background(), "test query")

	if results != nil {
		t.Errorf("expected nil for empty results, got %d items", len(results))
	}
}

func TestSearXNGSearchHTTPError(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockSrv.Close()

	s := New(10, 10*time.Second, "", 0, "searxng", mockSrv.URL)
	results := s.Search(context.Background(), "test query")

	if results != nil {
		t.Errorf("expected nil on HTTP error, got %d items", len(results))
	}
}

func TestSearXNGSearchResultLimit(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{"title": "R1", "url": "https://e.com/1", "content": "C1"},
				{"title": "R2", "url": "https://e.com/2", "content": "C2"},
				{"title": "R3", "url": "https://e.com/3", "content": "C3"},
			},
		})
	}))
	defer mockSrv.Close()

	s := New(2, 10*time.Second, "", 0, "searxng", mockSrv.URL)
	results := s.Search(context.Background(), "test query")

	if len(results) != 2 {
		t.Errorf("expected 2 results (limited), got %d", len(results))
	}
}

func TestSearXNGSearchQueryParams(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "json" {
			t.Errorf("expected format=json, got %s", r.URL.Query().Get("format"))
		}
		if r.URL.Query().Get("language") != "zh-CN" {
			t.Errorf("expected language=zh-CN, got %s", r.URL.Query().Get("language"))
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}})
	}))
	defer mockSrv.Close()

	s := New(10, 10*time.Second, "", 0, "searxng", mockSrv.URL)
	s.Search(context.Background(), "test query")
}

func TestBingBackendStillWorks(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{"title": "SearXNG Result", "url": "https://e.com/1", "content": "should not reach here"},
			},
		})
	}))
	defer mockSrv.Close()

	s := New(10, 10*time.Second, "http://not-a-real-bing-server", 0, "bing", mockSrv.URL)
	_ = s.Search(context.Background(), "test query")
}
