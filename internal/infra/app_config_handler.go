package infra

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// AppConfigHandler expone flags de configuración para los clientes móviles.
type AppConfigHandler struct{}

func NewAppConfigHandler() *AppConfigHandler {
	return &AppConfigHandler{}
}

// Get: GET /api/app-config
//
// `debug_menu_enabled` sale de la variable de entorno APP_DEBUG_MENU
// (true/false). Si no existe o no es un booleano válido, queda apagado.
func (h *AppConfigHandler) Get(c *gin.Context) {
	enabled, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv("APP_DEBUG_MENU")))
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"debug_menu_enabled": enabled})
}
