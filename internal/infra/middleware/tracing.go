package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hamp/booking-sport/pkg/logger"
)

type ResponseWriter struct {
	gin.ResponseWriter
	status int
	size   int
}

func (rw *ResponseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *ResponseWriter) Write(b []byte) (int, error) {
	size, err := rw.ResponseWriter.Write(b)
	rw.size += size
	return size, err
}

func Tracing() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		traceID := c.GetHeader("X-Trace-ID")
		if traceID == "" {
			traceID = uuid.New().String()
		}

		spanID := uuid.New().String()

		ctx := logger.WithTraceID(c.Request.Context(), traceID)
		ctx = logger.WithSpanID(ctx, spanID)
		c.Request = c.Request.WithContext(ctx)

		c.Header("X-Trace-ID", traceID)

		rw := &ResponseWriter{
			ResponseWriter: c.Writer,
			status:         http.StatusOK,
		}
		c.Writer = rw

		cfRay := c.GetHeader("Cf-Ray")
		cfIPCountry := c.GetHeader("Cf-Ipcountry")
		cfIPCity := c.GetHeader("Cf-Ipcity")
		cfIPContinent := c.GetHeader("Cf-Ipcontinent")
		cfIPLatitude := c.GetHeader("Cf-Iplatitude")
		cfIPLongitude := c.GetHeader("Cf-Iplongitude")
		cfConnectingIP := c.GetHeader("Cf-Connecting-IP")
		cfVisitor := c.GetHeader("Cf-Visitor")

		clientIP := c.ClientIP()
		if cfConnectingIP != "" {
			clientIP = cfConnectingIP
		}

		logger.FromContext(ctx).Infow("request_started",
			"msg", fmt.Sprintf("Solicitud %s %s recibida desde %s", c.Request.Method, c.Request.URL.Path, clientIP),
			"trace_id", traceID,
			"span_id", spanID,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"query", c.Request.URL.RawQuery,
			"client_ip", clientIP,
			"user_agent", c.Request.UserAgent(),
			"cf_ray", cfRay,
			"cf_ipcountry", cfIPCountry,
			"cf_ipcity", cfIPCity,
			"cf_ipcontinent", cfIPContinent,
			"cf_iplatitude", cfIPLatitude,
			"cf_iplongitude", cfIPLongitude,
			"cf_visitor", cfVisitor,
		)

		c.Next()

		duration := time.Since(start)

		logger.FromContext(ctx).Infow("request_completed",
			"msg", fmt.Sprintf("Solicitud %s %s completada — %d, %dms, %dB",
				c.Request.Method, c.Request.URL.Path, rw.status, duration.Milliseconds(), rw.size),
			"trace_id", traceID,
			"span_id", spanID,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status_code", rw.status,
			"duration_ms", duration.Milliseconds(),
			"duration_us", duration.Microseconds(),
			"response_size", rw.size,
			"client_ip", clientIP,
			"cf_ray", cfRay,
			"cf_ipcountry", cfIPCountry,
		)
	}
}

func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				traceID := logger.GetTraceID(c.Request.Context())

			logger.FromContext(c.Request.Context()).Errorw("panic_recovered",
				"msg", fmt.Sprintf("PANIC recuperado en %s %s", c.Request.Method, c.Request.URL.Path),
				"trace_id", traceID,
				"error", err,
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
			)

				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":   "internal_server_error",
					"message": "An unexpected error occurred",
					"trace_id": traceID,
				})
			}
		}()

		c.Next()
	}
}
