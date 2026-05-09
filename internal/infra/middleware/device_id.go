package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hamp/booking-sport/pkg/logger"
)

func isLocalhost(host string) bool {
	return host == "" ||
		strings.HasPrefix(host, "localhost") ||
		strings.HasPrefix(host, "127.0.0.1") ||
		strings.HasPrefix(host, "[::1]")
}

func DeviceID() gin.HandlerFunc {
	return func(c *gin.Context) {
		deviceID, err := c.Cookie("guest_device_id")
		if err != nil || deviceID == "" {
			deviceID = uuid.New().String()

			isHTTPS := c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"

			sameSite := http.SameSiteNoneMode
			secure := isHTTPS

			if !isHTTPS && !isLocalhost(c.Request.Host) {
				sameSite = http.SameSiteLaxMode
			}

			c.SetSameSite(sameSite)
			c.SetCookie(
				"guest_device_id",
				deviceID,
				365*24*3600,
				"/",
				"",
				secure,
				true,
			)

			logger.FromContext(c.Request.Context()).Infow("device_id_created",
				"msg", fmt.Sprintf("Nuevo guest_device_id asignado: %s", deviceID),
				"device_id", deviceID,
				"same_site", sameSite,
				"secure", secure,
				"host", c.Request.Host,
			)
		} else {
			logger.FromContext(c.Request.Context()).Infow("device_id_reused",
				"msg", fmt.Sprintf("guest_device_id reutilizado: %s", deviceID),
				"device_id", deviceID,
			)
		}

		c.Set("guest_device_id", deviceID)
		c.Next()
	}
}
