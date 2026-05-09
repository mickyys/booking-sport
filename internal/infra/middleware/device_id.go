package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
		}

		c.Set("guest_device_id", deviceID)
		c.Next()
	}
}
