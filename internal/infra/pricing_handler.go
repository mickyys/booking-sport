package infra

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
)

// PricingHandler expone la cotización pública con descuentos. Es de solo
// lectura: la reserva vuelve a cotizar en el servidor.
type PricingHandler struct {
	baseHandler *BaseHandler
	pricing     *app.PricingService
}

func NewPricingHandler(pricing *app.PricingService) *PricingHandler {
	return &PricingHandler{baseHandler: NewBaseHandler(), pricing: pricing}
}

type quoteRequest struct {
	CourtID         string `json:"court_id" binding:"required"`
	Date            string `json:"date" binding:"required"`
	Hour            int    `json:"hour"`
	Minutes         int    `json:"minutes"`
	DurationMinutes int    `json:"duration_minutes"`
	DiscountCode    string `json:"discount_code"`
	MemberToken     string `json:"member_token"`
	Email           string `json:"email"`
	DeviceID        string `json:"device_id"`
}

// Quote cotiza una reserva aplicando el mejor descuento disponible.
func (h *PricingHandler) Quote(c *gin.Context) {
	var input quoteRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "datos de cotización incompletos"})
		return
	}
	courtID, err := parseObjectID(input.CourtID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "court_id inválido"})
		return
	}
	date, err := parseQuoteDate(input.Date)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fecha inválida"})
		return
	}
	result, err := h.pricing.Quote(c.Request.Context(), app.QuoteInput{
		CourtID:         courtID,
		Date:            date,
		Hour:            input.Hour,
		Minutes:         input.Minutes,
		DurationMinutes: input.DurationMinutes,
		DiscountCode:    input.DiscountCode,
		MemberToken:     input.MemberToken,
		IdentityEmail:   input.Email,
		IdentityDevice:  input.DeviceID,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result.Quote)
}

func parseQuoteDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	return domain.ParseSantiagoDate(value)
}
