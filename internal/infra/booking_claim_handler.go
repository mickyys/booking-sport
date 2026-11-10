package infra

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// GuestBookingClaimer vincula reservas hechas como invitado a una cuenta.
type GuestBookingClaimer interface {
	ClaimByEmail(ctx context.Context, email, userID string) (int64, error)
}

// Auth0EmailLookup resuelve el correo de un usuario a partir de su `sub`.
//
// El access token no siempre trae el claim `email` (Auth0 solo lo incluye en
// el ID token salvo que una Action lo agregue al access token), así que el
// claim es el fallback para no fallar en silencio.
type Auth0EmailLookup interface {
	EmailByUserID(ctx context.Context, userID string) (string, error)
}

type BookingClaimHandler struct {
	claimer     GuestBookingClaimer
	users       Auth0EmailLookup
	baseHandler *BaseHandler
}

func NewBookingClaimHandler(claimer GuestBookingClaimer, users Auth0EmailLookup) *BookingClaimHandler {
	return &BookingClaimHandler{
		claimer:     claimer,
		users:       users,
		baseHandler: NewBaseHandler(),
	}
}

// Claim asocia al usuario autenticado las reservas de invitado hechas con su correo.
func (h *BookingClaimHandler) Claim(c *gin.Context) {
	log := h.baseHandler.GetLogger(c)

	userID := h.baseHandler.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found in token"})
		return
	}

	email, _ := c.Get("user_email")
	emailStr, _ := email.(string)
	emailStr = strings.TrimSpace(emailStr)

	if emailStr == "" && h.users != nil {
		resolved, err := h.users.EmailByUserID(c.Request.Context(), userID)
		if err != nil {
			log.Warnw("booking_claim_email_lookup_failed",
				"msg", "No se pudo resolver el correo del usuario para vincular reservas",
				"user_id", userID,
				"error", err,
			)
		} else {
			emailStr = strings.TrimSpace(resolved)
		}
	}

	if emailStr == "" {
		log.Warnw("booking_claim_without_email",
			"msg", "Claim sin correo: no se pueden vincular reservas de invitado",
			"user_id", userID,
		)
		c.JSON(http.StatusOK, gin.H{"claimed": 0, "reason": "email_unavailable"})
		return
	}

	claimed, err := h.claimer.ClaimByEmail(c.Request.Context(), emailStr, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Infow("booking_claim_completed",
		"msg", "Reservas de invitado vinculadas a la cuenta",
		"user_id", userID,
		"claimed", claimed,
	)

	c.JSON(http.StatusOK, gin.H{"claimed": claimed})
}
