package httpapi

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

var requestCount uint64
var errorCount uint64

func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		atomic.AddUint64(&requestCount, 1)
		if c.Writer.Status() >= 500 {
			atomic.AddUint64(&errorCount, 1)
		}
		c.Header("Server-Timing", fmt.Sprintf("app;dur=%d", time.Since(started).Microseconds()))
	}
}

func Metrics(c *gin.Context) {
	c.Header("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(c.Writer, "k12edu_http_requests_total %d\n", atomic.LoadUint64(&requestCount))
	fmt.Fprintf(c.Writer, "k12edu_http_errors_total %d\n", atomic.LoadUint64(&errorCount))
}
