package infra

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/internal/infra/auth0mgmt"
)

// centerFinder resuelve los centros donde participa un usuario. Lo implementa
// *app.SportCenterUseCase.
type centerFinder interface {
	FindByUserID(ctx context.Context, userID string) ([]domain.SportCenter, error)
}

// AccountHandler gestiona la eliminación de la propia cuenta del usuario
// autenticado.
type AccountHandler struct {
	auth0       auth0mgmt.Client
	centers     centerFinder
	baseHandler *BaseHandler
}

func NewAccountHandler(auth0 auth0mgmt.Client, centers centerFinder) *AccountHandler {
	return &AccountHandler{
		auth0:       auth0,
		centers:     centers,
		baseHandler: NewBaseHandler(),
	}
}

// DeleteMyAccount elimina la cuenta del usuario autenticado en Auth0.
//
//	DELETE /users/me/account
//
// No borra datos de Mongo: las reservas y registros existentes se conservan.
// Si el usuario es el único administrador de un centro, se rechaza para no
// dejar el centro sin acceso.
func (h *AccountHandler) DeleteMyAccount(c *gin.Context) {
	userID := h.baseHandler.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found in token"})
		return
	}

	if h.centers != nil {
		centers, err := h.centers.FindByUserID(c.Request.Context(), userID)
		if err != nil {
			h.baseHandler.LogError(c, "delete_account_centers_failed", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo validar tu centro"})
			return
		}
		for _, center := range centers {
			if len(center.Users) <= 1 {
				c.JSON(http.StatusConflict, gin.H{
					"error": "eres el único administrador de tu centro; agrega otro administrador antes de eliminar tu cuenta",
				})
				return
			}
		}
	}

	if err := h.auth0.DeleteUser(c.Request.Context(), userID); err != nil {
		h.baseHandler.LogError(c, "delete_account_failed", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "no se pudo eliminar la cuenta en este momento, intenta nuevamente"})
		return
	}

	h.baseHandler.LogRequest(c, "account_deleted")
	c.JSON(http.StatusOK, gin.H{"message": "Cuenta eliminada"})
}
