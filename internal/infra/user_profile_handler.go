package infra

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
)

type UserProfileHandler struct {
	useCase     *app.UserProfileUseCase
	baseHandler *BaseHandler
}

func NewUserProfileHandler(uc *app.UserProfileUseCase) *UserProfileHandler {
	return &UserProfileHandler{useCase: uc, baseHandler: NewBaseHandler()}
}

// GetPhone devuelve el teléfono guardado del usuario autenticado.
func (h *UserProfileHandler) GetPhone(c *gin.Context) {
	userID := h.baseHandler.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found in token"})
		return
	}
	phone, err := h.useCase.GetPhone(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"phone": phone})
}

// SavePhone guarda el teléfono del usuario autenticado.
func (h *UserProfileHandler) SavePhone(c *gin.Context) {
	userID := h.baseHandler.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found in token"})
		return
	}
	var req struct {
		Phone string `json:"phone" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	phone, err := h.useCase.SavePhone(c.Request.Context(), userID, req.Phone)
	if errors.Is(err, app.ErrInvalidPhone) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"phone": phone})
}
