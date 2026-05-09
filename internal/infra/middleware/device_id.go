package middleware

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hamp/booking-sport/pkg/logger"
)

func DeviceID() gin.HandlerFunc {
	return func(c *gin.Context) {
		deviceID, err := c.Cookie("guest_device_id")
		if err != nil || deviceID == "" {
			deviceID = uuid.New().String()

			sameSite := http.SameSiteLaxMode
			if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
				sameSite = http.SameSiteNoneMode
			}

			c.SetSameSite(sameSite)
			c.SetCookie(
				"guest_device_id",
				deviceID,
				365*24*3600,
				"/",
				"",
				c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https",
				true,
			)

			logger.FromContext(c.Request.Context()).Infow("device_id_created",
				"msg", fmt.Sprintf("Nuevo guest_device_id asignado: %s", deviceID),
				"device_id", deviceID,
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
