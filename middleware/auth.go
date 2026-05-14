package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" {
			c.Set("auth_header", "Bearer public")
			c.Next()
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		if token == "" || token == auth {
			c.Set("auth_header", "Bearer public")
		} else {
			c.Set("auth_header", auth)
		}
		c.Next()
	}
}

func GetAuthHeader(c *gin.Context) string {
	auth, _ := c.Get("auth_header")
	if auth == nil {
		return "Bearer public"
	}
	return auth.(string)
}
