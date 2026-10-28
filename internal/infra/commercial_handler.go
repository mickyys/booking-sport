package infra

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// CommercialHandler expone el modelo comercial: modalidad, suscripción,
// comisión y publicación.
type CommercialHandler struct {
	useCase     *app.CommercialUseCase
	baseHandler *BaseHandler
}

func NewCommercialHandler(uc *app.CommercialUseCase) *CommercialHandler {
	return &CommercialHandler{useCase: uc, baseHandler: NewBaseHandler()}
}

func commercialError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		status = appErr.StatusCode
	}
	c.JSON(status, gin.H{"error": err.Error()})
}

// adminCenterID resuelve el centro del usuario autenticado.
func (h *CommercialHandler) adminCenterID(c *gin.Context) (primitive.ObjectID, bool) {
	userID := h.baseHandler.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found in token"})
		return primitive.NilObjectID, false
	}
	id, err := h.useCase.CenterIDForUser(c.Request.Context(), userID)
	if err != nil {
		commercialError(c, err)
		return primitive.NilObjectID, false
	}
	return id, true
}

func paramCenterID(c *gin.Context) (primitive.ObjectID, bool) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return primitive.NilObjectID, false
	}
	return id, true
}

// --- Administrador del centro ---

func (h *CommercialHandler) Get(c *gin.Context) {
	id, ok := h.adminCenterID(c)
	if !ok {
		return
	}
	v, err := h.useCase.Get(c.Request.Context(), id)
	if err != nil {
		commercialError(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *CommercialHandler) PublishCheck(c *gin.Context) {
	id, ok := h.adminCenterID(c)
	if !ok {
		return
	}
	check, err := h.useCase.ValidatePublish(c.Request.Context(), id)
	if err != nil {
		commercialError(c, err)
		return
	}
	c.JSON(http.StatusOK, check)
}

func (h *CommercialHandler) Publish(c *gin.Context) {
	id, ok := h.adminCenterID(c)
	if !ok {
		return
	}
	v, err := h.useCase.Publish(c.Request.Context(), id)
	if err != nil {
		commercialError(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *CommercialHandler) SetMode(c *gin.Context) {
	id, ok := h.adminCenterID(c)
	if !ok {
		return
	}
	h.setMode(c, id)
}

func (h *CommercialHandler) StartSubscription(c *gin.Context) {
	id, ok := h.adminCenterID(c)
	if !ok {
		return
	}
	var body struct {
		PayerEmail string `json:"payer_email"`
	}
	_ = c.ShouldBindJSON(&body)
	res, err := h.useCase.StartSubscription(c.Request.Context(), id, body.PayerEmail)
	if err != nil {
		h.baseHandler.LogError(c, "subscription_start_error", err, "center_id", id.Hex())
		commercialError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *CommercialHandler) GetSubscription(c *gin.Context) {
	id, ok := h.adminCenterID(c)
	if !ok {
		return
	}
	v, err := h.useCase.Get(c.Request.Context(), id)
	if err != nil {
		commercialError(c, err)
		return
	}
	if v.Subscription == nil {
		c.JSON(http.StatusOK, gin.H{"status": domain.SubscriptionNotConfigured, "applicable": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"applicable": true, "subscription": v.Subscription})
}

func (h *CommercialHandler) setMode(c *gin.Context, id primitive.ObjectID) {
	var body struct {
		PaymentMode string `json:"payment_mode" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	v, err := h.useCase.SetMode(c.Request.Context(), id, body.PaymentMode)
	if err != nil {
		commercialError(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

// --- Superadmin ---

func (h *CommercialHandler) AdminGet(c *gin.Context) {
	id, ok := paramCenterID(c)
	if !ok {
		return
	}
	v, err := h.useCase.Get(c.Request.Context(), id)
	if err != nil {
		commercialError(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *CommercialHandler) AdminSetMode(c *gin.Context) {
	id, ok := paramCenterID(c)
	if !ok {
		return
	}
	h.setMode(c, id)
}

// AdminSetCommission fija la comisión del centro; `value: null` la restablece
// al default global.
func (h *CommercialHandler) AdminSetCommission(c *gin.Context) {
	id, ok := paramCenterID(c)
	if !ok {
		return
	}
	var body struct {
		Value *int `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	v, err := h.useCase.SetCommission(c.Request.Context(), id, body.Value)
	if err != nil {
		commercialError(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *CommercialHandler) AdminPublish(c *gin.Context) {
	id, ok := paramCenterID(c)
	if !ok {
		return
	}
	v, err := h.useCase.Publish(c.Request.Context(), id)
	if err != nil {
		commercialError(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

// --- Webhook público de Mercado Pago (suscripciones) ---

// SubscriptionWebhook recibe notificaciones `subscription_preapproval`. El
// estado se vuelve a consultar a Mercado Pago, por lo que un cuerpo falso no
// puede activar una suscripción.
func (h *CommercialHandler) SubscriptionWebhook(c *gin.Context) {
	var body struct {
		Type string `json:"type"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = c.ShouldBindJSON(&body)
	typ, id := body.Type, body.Data.ID
	if typ == "" {
		typ = c.Query("type")
	}
	if id == "" {
		id = c.Query("data.id")
	}
	if typ != "subscription_preapproval" || id == "" {
		c.Status(http.StatusOK)
		return
	}
	if err := h.useCase.HandleSubscriptionWebhook(c.Request.Context(), id); err != nil {
		if errors.Is(err, app.ErrSubscriptionIgnored) {
			c.Status(http.StatusOK)
			return
		}
		h.baseHandler.LogError(c, "subscription_webhook_error", err, "preapproval_id", id)
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusOK)
}
