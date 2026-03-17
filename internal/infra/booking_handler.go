package infra

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type BookingHandler struct {
	useCase *app.BookingUseCase
}

func NewBookingHandler(uc *app.BookingUseCase) *BookingHandler {
	return &BookingHandler{useCase: uc}
}

func (h *BookingHandler) CreateFintocPaymentIntent(c *gin.Context) {
	var booking domain.Booking
	if err := c.ShouldBindJSON(&booking); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	redirectURL, err := h.useCase.CreateFintocPaymentIntent(c.Request.Context(), &booking)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"redirect_url": redirectURL})
}

func (h *BookingHandler) FintocWebhook(c *gin.Context) {
	var event struct {
		ID   string          `json:"id"`
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}

	if err := c.ShouldBindJSON(&event); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Logs para debugging del webhook
	fmt.Printf("[FINTOC WEBHOOK] Evento recibido: %s\n", event.Type)

	switch event.Type {
	case "checkout_session.finished":
		var data struct {
			ID              string `json:"id"`
			Status          string `json:"status"`
			PaymentResource struct {
				PaymentIntent struct {
					ID string `json:"id"`
				} `json:"payment_intent"`
			} `json:"payment_resource"`
		}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if data.Status == "finished" {
			paymentIntentID := data.PaymentResource.PaymentIntent.ID
			checkoutSessionID := data.ID
			err := h.useCase.HandleFintocCheckoutFinished(c.Request.Context(), checkoutSessionID, paymentIntentID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
	case "payment_intent.succeeded":
		var data struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := h.useCase.HandleFintocWebhook(c.Request.Context(), data.ID, "succeeded")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	case "payment_intent.failed":
		var data struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := h.useCase.HandleFintocWebhook(c.Request.Context(), data.ID, "failed")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	case "refund.succeeded":
		var data struct {
			ID         string `json:"id"`
			Amount     int    `json:"amount"`
			Status     string `json:"status"`
			ResourceID string `json:"resource_id"` // Fintoc payment_intent_id
		}
		if err := json.Unmarshal(event.Data, &data); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := h.useCase.HandleFintocRefund(c.Request.Context(), data.ResourceID, data.ID, data.Amount, data.Status)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "received"})
}

func (h *BookingHandler) GetFintocPaymentIntentStatus(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "payment intent id is required"})
		return
	}

	status, err := h.useCase.GetFintocPaymentStatus(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": status})
}

func (h *BookingHandler) FintocReturn(c *gin.Context) {
	url := os.Getenv("URL_FRONTEND")
	bookingCode := c.Query("id") // Fintoc envía el id en el query param 'id', que mapeamos al booking_code
	fmt.Println("============== ID =================", bookingCode)
	if bookingCode == "" {
		c.Redirect(http.StatusFound, url+"/booking/failure?error=missing_id")
		return
	}

	code, err := h.useCase.ValidateFintocPaymentAndGetCode(c.Request.Context(), bookingCode)
	if err != nil {
		c.Redirect(http.StatusFound, url+"/booking/failure?error=not_found")
		return
	}

	// Redirigir al front con el código único de la reserva
	redirectURL := fmt.Sprintf("%s/booking?code=%s", url, code)
	c.Redirect(http.StatusFound, redirectURL)
}

func (h *BookingHandler) GetConfirmedCount(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found in token"})
		return
	}

	count, err := h.useCase.GetConfirmedBookingCount(c.Request.Context(), userID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": count})
}

func (h *BookingHandler) GetUserBookings(c *gin.Context) {
	// Obtener userID del token (inyectado por el middleware)
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found in token"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	isOld := c.Query("old") == "true"

	bookings, total, err := h.useCase.GetUserBookingsPaged(c.Request.Context(), userID.(string), page, limit, isOld)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	totalPages := int(math.Ceil(float64(total) / float64(limit)))

	c.JSON(http.StatusOK, domain.PagedResponse{
		Data:       bookings,
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: totalPages,
	})
}

func (h *BookingHandler) CancelBooking(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "booking id is required"})
		return
	}

	bookingID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid booking id format"})
		return
	}

	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found in token"})
		return
	}

	err = h.useCase.CancelBooking(c.Request.Context(), bookingID, userID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

func (h *BookingHandler) GetByBookingCode(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "booking code is required"})
		return
	}

	booking, err := h.useCase.GetByBookingCode(c.Request.Context(), code)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "booking not found"})
		return
	}

	c.JSON(http.StatusOK, booking)
}
