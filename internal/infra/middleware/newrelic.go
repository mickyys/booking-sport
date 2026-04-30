package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/newrelic/go-agent/v3/integrations/nrgin"
	"github.com/hamp/booking-sport/pkg/newrelic"
)

func NewRelicMiddleware() gin.HandlerFunc {
	nrApp := newrelic.GetApp()
	if nrApp == nil {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return nrgin.Middleware(nrApp)
}
