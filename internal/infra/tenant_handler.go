package infra

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/mongo"
)

type TenantHandler struct {
	useCase *app.TenantUseCase
}

func NewTenantHandler(uc *app.TenantUseCase) *TenantHandler {
	return &TenantHandler{useCase: uc}
}

// GetByHost resuelve un hostname (ej: catolica.reservaloya.cl o www.unioncatolica.cl)
// hacia la configuración pública de su tenant.
func (h *TenantHandler) GetByHost(c *gin.Context) {
	host := c.Param("host")
	tenant, err := h.useCase.ResolveByHost(c.Request.Context(), host)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) || errors.Is(err, mongo.ErrNoDocuments) {
			c.JSON(http.StatusNotFound, gin.H{"error": "tenant not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tenant})
}

// GetBySubdomain resuelve un tenant por su subdominio canónico (ej: "catolica").
func (h *TenantHandler) GetBySubdomain(c *gin.Context) {
	subdomain := c.Param("subdomain")
	tenant, err := h.useCase.ResolveBySubdomain(c.Request.Context(), subdomain)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) || errors.Is(err, mongo.ErrNoDocuments) {
			c.JSON(http.StatusNotFound, gin.H{"error": "tenant not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tenant})
}
