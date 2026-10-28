package infra

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// GuestBookingClaimer vincula reservas hechas como invitado a una cuenta.
type GuestBookingClaimer interface {
	ClaimByEmail(ctx context.Context, email, userID string) (int64, error)
}

type BookingClaimHandler struct {
	claimer     GuestBookingClaimer
	baseHandler *BaseHandler
}

func NewBookingClaimHandler(claimer GuestBookingClaimer) *BookingClaimHandler {
	return &BookingClaimHandler{claimer: claimer, baseHandler: NewBaseHandler()}
}

// Claim asocia al usuario autenticado las reservas de invitado hechas con su correo.
func (h *BookingClaimHandler) Claim(c *gin.Context) {
	userID := h.baseHandler.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found in token"})
		return
	}
	email, _ := c.Get("user_email")
	emailStr, _ := email.(string)
	if emailStr == "" {
		c.JSON(http.StatusOK, gin.H{"claimed": 0})
		return
	}
	claimed, err := h.claimer.ClaimByEmail(c.Request.Context(), emailStr, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"claimed": claimed})
}
