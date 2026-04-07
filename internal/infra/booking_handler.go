package infra

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/fintoc"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type BookingHandler struct {
	useCase *app.BookingUseCase
}

func NewBookingHandler(uc *app.BookingUseCase) *BookingHandler {
	return &BookingHandler{useCase: uc}
}

func (h *BookingHandler) GetUserCancelledBookings(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found in token"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	bookings, total, err := h.useCase.GetUserCancelledBookingsPaged(c.Request.Context(), userID.(string), page, limit)
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

func (h *BookingHandler) GetRecurringSeries(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "admin_id not found in token"})
		return
	}

	userIDStr := userID.(string)

	series, err := h.useCase.GetRecurringSeries(c.Request.Context(), userIDStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": series})
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
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var event struct {
		ID   string          `json:"id"`
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(bodyBytes, &event); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	signature := c.GetHeader("Fintoc-Signature")
	if signature != "" {
		var data struct {
			ID string `json:"id"`
		}
		json.Unmarshal(event.Data, &data)

		secret, err := h.useCase.GetWebhookSecret(c.Request.Context(), data.ID)

		if err != nil {
			log.Printf("[FINTOC WEBHOOK] ERROR GETTING SECRET for ID %s: %v\n", data.ID, err)
		}

		if err == nil {
			if !fintoc.VerifySignature(bodyBytes, signature, secret) {
				log.Printf("[FINTOC WEBHOOK] INVALID SIGNATURE for ID: %s\n", data.ID)
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
				return
			}
			booking, errB := h.useCase.GetBookingByFintocID(c.Request.Context(), data.ID)
			if errB == nil && booking != nil {
				center, _ := h.useCase.GetSportCenterByID(c.Request.Context(), booking.SportCenterID)
				centerName := "Unknown"
				if center != nil {
					centerName = center.Name
				}
				log.Printf("[FINTOC WEBHOOK] VALID SIGNATURE - Center: %s, Amount: %.2f, Event: %s, ID: %s\n",
					centerName, booking.Price, event.Type, data.ID)
			} else {
				log.Printf("[FINTOC WEBHOOK] VALID SIGNATURE (Unknown Booking) - Event: %s, ID: %s\n", event.Type, data.ID)
			}
		}
	}

	log.Printf("[FINTOC WEBHOOK] Evento recibido: %s\n", event.Type)

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
	bookingCode := c.Query("id")
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
	redirectURL := fmt.Sprintf("%s/booking/status?code=%s", url, code)
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

func (h *BookingHandler) DeleteBookingSeries(c *gin.Context) {
	seriesID := c.Param("series_id")
	if seriesID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "series_id is required"})
		return
	}

	err := h.useCase.DeleteSeries(c.Request.Context(), seriesID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Series deleted successfully"})
}

func (h *BookingHandler) GetBookingDetail(c *gin.Context) {
	log.Printf(" ========== GetBookingDetail ==========")
	idStr := c.Param("id")
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid booking id"})
		return
	}

	booking, err := h.useCase.GetByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "booking not found"})
		return
	}
	log.Printf("booking =========> %+v\n", booking)
	court, err := h.useCase.GetCourtByID(c.Request.Context(), booking.CourtID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get court info"})
		return
	}

	log.Printf("court =========> %+v\n", court)

	center, err := h.useCase.GetSportCenterByID(c.Request.Context(), court.SportCenterID)
	if err != nil {
		log.Printf("[GET_BOOKING_DETAIL] Error obteniendo centro %s para reserva %s: %v\n", court.SportCenterID.Hex(), booking.ID.Hex(), err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":     "failed to get sport center info",
			"details":   err.Error(),
			"center_id": court.SportCenterID.Hex(),
		})
		return
	}
	hoursUntilMatch := time.Until(booking.Date.Add(time.Hour * time.Duration(booking.Hour))).Hours()

	configCancellationHours := center.CancellationHours
	if configCancellationHours == 0 {
		configCancellationHours = 3 // default
	}
	configRetentionPercent := center.RetentionPercent
	if configRetentionPercent == 0 {
		configRetentionPercent = 10 // default
	}

	canCancel := hoursUntilMatch > 0 && booking.Status == domain.BookingStatusConfirmed
	refundPercentage := 0
	if canCancel {
		if hoursUntilMatch >= float64(configCancellationHours) {
			refundPercentage = 100
		} else {
			refundPercentage = 100 - configRetentionPercent
		}
	}

	maxRefundAmount := (booking.Price * float64(refundPercentage)) / 100

	response := gin.H{
		"booking_detail": gin.H{
			"id":                   booking.ID,
			"user_id":              booking.UserID,
			"court_id":             booking.CourtID,
			"court_name":           court.Name,
			"sport_center_id":      court.SportCenterID,
			"sport_center_name":    center.Name,
			"date":                 booking.Date,
			"hour":                 booking.Hour,
			"price":                booking.Price,
			"paid_amount":          booking.PaidAmount,
			"pending_amount":       booking.PendingAmount,
			"is_partial_payment":   booking.IsPartialPayment,
			"partial_payment_paid": booking.PartialPaymentPaid,
			"status":               booking.Status,
			"payment_method":       booking.PaymentMethod,
			"booking_code":         booking.BookingCode,
			"created_at":           booking.CreatedAt,
			"updated_at":           booking.UpdatedAt,
		},
		"hours_until_match": hoursUntilMatch,
		"can_cancel":        canCancel,
		"refund_percentage": refundPercentage,
		"amount_paid":       booking.Price,
		"max_refund_amount": maxRefundAmount,
		"cancellation_policy": gin.H{
			"limit_hours":       configCancellationHours,
			"retention_percent": configRetentionPercent,
		},
	}

	c.JSON(http.StatusOK, response)
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

	court, err := h.useCase.GetCourtByID(c.Request.Context(), booking.CourtID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get court info"})
		return
	}

	center, err := h.useCase.GetSportCenterByID(c.Request.Context(), court.SportCenterID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get sport center info"})
		return
	}

	hoursUntilMatch := time.Until(booking.Date.Add(time.Hour * time.Duration(booking.Hour))).Hours()

	configCancellationHours := center.CancellationHours
	if configCancellationHours == 0 {
		configCancellationHours = 3
	}
	configRetentionPercent := center.RetentionPercent
	if configRetentionPercent == 0 {
		configRetentionPercent = 10
	}

	canCancel := hoursUntilMatch > 0 && booking.Status == domain.BookingStatusConfirmed
	refundPercentage := 0
	if canCancel {
		if hoursUntilMatch >= float64(configCancellationHours) {
			refundPercentage = 100
		} else {
			refundPercentage = 100 - configRetentionPercent
		}
	}

	maxRefundAmount := (booking.Price * float64(refundPercentage)) / 100

	response := gin.H{
		"booking_detail": gin.H{
			"id":                   booking.ID,
			"user_id":              booking.UserID,
			"court_id":             booking.CourtID,
			"court_name":           court.Name,
			"sport_center_id":      court.SportCenterID,
			"sport_center_name":    center.Name,
			"date":                 booking.Date,
			"hour":                 booking.Hour,
			"price":                booking.Price,
			"paid_amount":          booking.PaidAmount,
			"pending_amount":       booking.PendingAmount,
			"is_partial_payment":   booking.IsPartialPayment,
			"partial_payment_paid": booking.PartialPaymentPaid,
			"status":               booking.Status,
			"payment_method":       booking.PaymentMethod,
			"booking_code":         booking.BookingCode,
			"created_at":           booking.CreatedAt,
			"updated_at":           booking.UpdatedAt,
		},
		"hours_until_match": hoursUntilMatch,
		"can_cancel":        canCancel,
		"refund_percentage": refundPercentage,
		"amount_paid":       booking.Price,
		"max_refund_amount": maxRefundAmount,
		"cancellation_policy": gin.H{
			"limit_hours":       configCancellationHours,
			"retention_percent": configRetentionPercent,
		},
	}

	c.JSON(http.StatusOK, response)
}

func (h *BookingHandler) CancelByBookingCode(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "booking code is required"})
		return
	}

	booking, err := h.useCase.GetByBookingCode(c.Request.Context(), code)
	if err != nil || booking == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "booking not found"})
		return
	}

	err = h.useCase.CancelBooking(c.Request.Context(), booking.ID, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

func (h *BookingHandler) CreateInternalBooking(c *gin.Context) {
	var booking domain.Booking
	if err := c.ShouldBindJSON(&booking); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := h.useCase.CreateInternalBooking(c.Request.Context(), &booking)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, booking)
}

func (h *BookingHandler) CreateBooking(c *gin.Context) {
	var booking struct {
		domain.Booking
		SeriesID string `json:"series_id"`
	}
	if err := c.ShouldBindJSON(&booking); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	booking.Booking.SeriesID = booking.SeriesID

	if userID, exists := c.Get("user_id"); exists {
		booking.Booking.UserID = userID.(string)
	}

	err := h.useCase.CreateInternalBooking(c.Request.Context(), &booking.Booking)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, booking)
}

func (h *BookingHandler) DeleteBooking(c *gin.Context) {
	idStr := c.Param("id")
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid booking id"})
		return
	}

	err = h.useCase.DeleteBooking(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (h *BookingHandler) MarkAsPaidInPerson(c *gin.Context) {
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

	err = h.useCase.MarkAsPaidInPerson(c.Request.Context(), bookingID, userID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "paid"})
}

func (h *BookingHandler) GetAdminDashboard(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found in token"})
		return
	}

	userIDStr, ok := userID.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid user_id type"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	date := c.Query("date")
	name := c.Query("name")
	code := c.Query("code")
	status := c.Query("status")

	data, err := h.useCase.GetAdminDashboard(c.Request.Context(), userIDStr, page, limit, date, name, code, status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, data)
}

// ==================== MercadoPago Handlers ====================

func (h *BookingHandler) CreateMercadoPagoPayment(c *gin.Context) {
	var booking domain.Booking
	if err := c.ShouldBindJSON(&booking); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	initPoint, err := h.useCase.CreateMercadoPagoPayment(c.Request.Context(), &booking)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"init_point": initPoint})
}

func (h *BookingHandler) MercadoPagoWebhook(c *gin.Context) {
	var event struct {
		Action string `json:"action"`
		Type   string `json:"type"`
		Data   struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := c.ShouldBindJSON(&event); err != nil {
		topic := c.Query("topic")
		id := c.Query("id")
		if topic == "payment" && id != "" {
			err := h.useCase.HandleMercadoPagoWebhook(c.Request.Context(), id)
			if err != nil {
				log.Printf("[MP WEBHOOK ERROR] %v\n", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "received"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ignored"})
		return
	}

	// Only process payment-related events
	if event.Type == "payment" || event.Action == "payment.created" || event.Action == "payment.updated" {
		if event.Data.ID != "" {
			err := h.useCase.HandleMercadoPagoWebhook(c.Request.Context(), event.Data.ID)
			if err != nil {
				log.Printf("[MP WEBHOOK ERROR] %v\n", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "received"})
}

func (h *BookingHandler) MercadoPagoReturn(c *gin.Context) {
	url := os.Getenv("URL_FRONTEND")
	bookingCode := c.Query("code")
	paymentID := c.Query("payment_id")
	log.Printf("[MP RETURN] code=%s payment_id=%s\n", bookingCode, paymentID)

	if bookingCode == "" {
		bookingCode = c.Query("external_reference")
	}

	if bookingCode == "" {
		c.Redirect(http.StatusFound, url+"/booking/failure?error=missing_code")
		return
	}

	if paymentID != "" {
		if err := h.useCase.StoreMPPaymentID(c.Request.Context(), bookingCode, paymentID); err != nil {
			log.Printf("[MP RETURN] Error storing payment_id %s for code %s: %v\n", paymentID, bookingCode, err)
		}
		if err := h.useCase.HandleMercadoPagoWebhook(c.Request.Context(), paymentID); err != nil {
			log.Printf("[MP RETURN] Error processing payment %s: %v\n", paymentID, err)
		}
	}

	code, err := h.useCase.ValidateMercadoPagoPaymentAndGetCode(c.Request.Context(), bookingCode)
	if err != nil {
		log.Printf("[MP RETURN] Error validating payment for code %s: %v\n", bookingCode, err)
		c.Redirect(http.StatusFound, url+"/booking/failure?error=validation_failed")
		return
	}

	redirectURL := fmt.Sprintf("%s/booking/status?code=%s", url, code)
	c.Redirect(http.StatusFound, redirectURL)
}
