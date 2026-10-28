package infra

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type FacilityBookingHandler struct {
	useCase *app.FacilityBookingUseCase
}

func NewFacilityBookingHandler(useCase *app.FacilityBookingUseCase) *FacilityBookingHandler {
	return &FacilityBookingHandler{useCase: useCase}
}

// Create reserva uno o más días de una sede o piscina (público).
func (h *FacilityBookingHandler) Create(c *gin.Context) {
	var body struct {
		SpaceID       string   `json:"space_id"`
		Dates         []string `json:"dates"`
		Guests        int      `json:"guests"`
		CustomerName  string   `json:"customer_name"`
		CustomerEmail string   `json:"customer_email"`
		CustomerPhone string   `json:"customer_phone"`
		PaymentMethod string   `json:"payment_method"`
		Partial       bool     `json:"partial"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	spaceID, err := primitive.ObjectIDFromHex(body.SpaceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "space_id inválido"})
		return
	}

	result, err := h.useCase.Create(c.Request.Context(), app.CreateFacilityBookingInput{
		SpaceID:       spaceID,
		Dates:         body.Dates,
		Guests:        body.Guests,
		CustomerName:  body.CustomerName,
		CustomerEmail: body.CustomerEmail,
		CustomerPhone: body.CustomerPhone,
		PaymentMethod: body.PaymentMethod,
		Partial:       body.Partial,
	})
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, result)
}

// PaymentOptions informa las modalidades de pago disponibles para un espacio (público).
func (h *FacilityBookingHandler) PaymentOptions(c *gin.Context) {
	spaceID, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return
	}
	opts, err := h.useCase.PaymentOptions(c.Request.Context(), spaceID)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, opts)
}

// ListAdmin lista las reservas de sede y piscina del centro del admin.
func (h *FacilityBookingHandler) ListAdmin(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return
	}
	bookings, err := h.useCase.ListForAdmin(c.Request.Context(), userID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"bookings": bookings})
}

// CancelAdmin cancela un día reservado y libera la fecha.
func (h *FacilityBookingHandler) CancelAdmin(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return
	}
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return
	}
	if err := h.useCase.CancelByAdmin(c.Request.Context(), id, userID.(string)); err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Reserva cancelada"})
}
