package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID := c.GetHeader("X-Request-ID")
		if reqID == "" || len(reqID) > 128 {
			reqID = uuid.NewString()
		}
		c.Writer.Header().Set("X-Request-ID", reqID)
		c.Set("request_id", reqID)
		c.Next()
	}
}
