package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const requestBodyLimitKey = "cliproxy.request_body_limit"

// RequestBodyLimit must run before middleware that buffers or logs request bodies.
// It also supplies the decoded-size limit to request logging.
func RequestBodyLimit(path string, limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || c.Request.URL.Path != path || limit <= 0 {
			c.Next()
			return
		}
		if c.Request.ContentLength > limit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": gin.H{
				"message": "request body exceeds the configured size limit", "type": "invalid_request_error",
			}})
			return
		}
		c.Set(requestBodyLimitKey, limit)
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}
