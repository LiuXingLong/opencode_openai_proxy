package test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/LiuXingLong/opencode-openai-proxy/middleware"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestAuthNoHeader(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req, _ := http.NewRequest("POST", "/v1/responses", nil)
	c.Request = req

	middleware.Auth()(c)

	got := middleware.GetAuthHeader(c)
	if got != "Bearer public" {
		t.Errorf("no Authorization header: expected Bearer public, got %s", got)
	}
}

func TestAuthEmptyHeader(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req, _ := http.NewRequest("POST", "/v1/responses", nil)
	req.Header.Set("Authorization", "")
	c.Request = req

	middleware.Auth()(c)

	got := middleware.GetAuthHeader(c)
	if got != "Bearer public" {
		t.Errorf("empty Authorization header: expected Bearer public, got %s", got)
	}
}

func TestAuthBearerNoToken(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req, _ := http.NewRequest("POST", "/v1/responses", nil)
	req.Header.Set("Authorization", "Bearer ")
	c.Request = req

	middleware.Auth()(c)

	got := middleware.GetAuthHeader(c)
	if got != "Bearer public" {
		t.Errorf("Bearer with no token: expected Bearer public, got %s", got)
	}
}

func TestAuthBearerWithToken(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req, _ := http.NewRequest("POST", "/v1/responses", nil)
	req.Header.Set("Authorization", "Bearer sk-test-key-12345")
	c.Request = req

	middleware.Auth()(c)

	got := middleware.GetAuthHeader(c)
	if got != "Bearer sk-test-key-12345" {
		t.Errorf("valid Bearer token: expected Bearer sk-test-key-12345, got %s", got)
	}
}

func TestAuthBearerWithRealToken(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	token := "sk-ant-sid01-xxxxx"
	req, _ := http.NewRequest("POST", "/v1/responses", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	c.Request = req

	middleware.Auth()(c)

	got := middleware.GetAuthHeader(c)
	if got != "Bearer "+token {
		t.Errorf("valid Bearer token: expected Bearer %s, got %s", token, got)
	}
}

func TestAuthNonBearerHeader(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req, _ := http.NewRequest("POST", "/v1/responses", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	c.Request = req

	middleware.Auth()(c)

	got := middleware.GetAuthHeader(c)
	if got != "Bearer public" {
		t.Errorf("non-Bearer header: expected Bearer public, got %s", got)
	}
}
