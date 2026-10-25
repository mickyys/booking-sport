package infra

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// MembershipHandler expone la verificación pública de socios y la
// administración de planes y socios de un centro.
type MembershipHandler struct {
	baseHandler  *BaseHandler
	membership   *app.MembershipUseCase
	verification *app.MemberVerificationService
	centerRepo   app.SportCenterRepository
}

func NewMembershipHandler(membership *app.MembershipUseCase, verification *app.MemberVerificationService, centerRepo app.SportCenterRepository) *MembershipHandler {
	return &MembershipHandler{
		baseHandler:  NewBaseHandler(),
		membership:   membership,
		verification: verification,
		centerRepo:   centerRepo,
	}
}

// --- Verificación pública ---

type verifyRequestInput struct {
	CenterID   string `json:"center_id"`
	CenterSlug string `json:"center_slug"`
	Document   string `json:"document" binding:"required"`
	Email      string `json:"email" binding:"required"`
}

// RequestVerification envía un código al correo registrado del socio.
func (h *MembershipHandler) RequestVerification(c *gin.Context) {
	var input verifyRequestInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "datos incompletos"})
		return
	}
	center, err := h.resolvePublicCenter(c, input.CenterID, input.CenterSlug)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	result, err := h.verification.RequestVerification(c.Request.Context(), center.ID, input.Document, input.Email, c.ClientIP())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no pudimos enviar el código, intenta nuevamente"})
		return
	}
	c.JSON(http.StatusOK, result)
}

type verifyConfirmInput struct {
	VerificationID string `json:"verification_id" binding:"required"`
	Code           string `json:"code" binding:"required"`
}

// ConfirmVerification valida el código y entrega el token de socio.
func (h *MembershipHandler) ConfirmVerification(c *gin.Context) {
	var input verifyConfirmInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "datos incompletos"})
		return
	}
	result, err := h.verification.ConfirmVerification(c.Request.Context(), input.VerificationID, input.Code)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// --- Administración de planes ---

type planRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Discount    domain.DiscountValue `json:"discount"`
	Scope       domain.DiscountScope `json:"scope"`
	UsageLimit  domain.UsageLimit    `json:"usage_limit"`
	Active      *bool                `json:"active"`
}

func (r planRequest) toPlan() *domain.MembershipPlan {
	active := true
	if r.Active != nil {
		active = *r.Active
	}
	return &domain.MembershipPlan{
		Name:        strings.TrimSpace(r.Name),
		Description: strings.TrimSpace(r.Description),
		Discount:    r.Discount,
		Scope:       r.Scope,
		UsageLimit:  r.UsageLimit,
		Active:      active,
	}
}

// ListPlans lista los planes del centro activo del organizador.
func (h *MembershipHandler) ListPlans(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	plans, err := h.membership.ListPlans(c.Request.Context(), center.ID)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

// CreatePlan crea un plan de socio.
func (h *MembershipHandler) CreatePlan(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	var input planRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "datos del plan inválidos"})
		return
	}
	plan, err := h.membership.CreatePlan(c.Request.Context(), center.ID, input.toPlan(), h.baseHandler.GetUserID(c))
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, plan)
}

// UpdatePlan actualiza un plan existente.
func (h *MembershipHandler) UpdatePlan(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	id, err := parseObjectID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return
	}
	var input planRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "datos del plan inválidos"})
		return
	}
	current, err := h.membership.GetPlan(c.Request.Context(), center.ID, id)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	plan := input.toPlan()
	if input.Active == nil {
		plan.Active = current.Active
	}
	updated, err := h.membership.UpdatePlan(c.Request.Context(), center.ID, id, plan)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// DeletePlan desactiva un plan.
func (h *MembershipHandler) DeletePlan(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	id, err := parseObjectID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return
	}
	if err := h.membership.DeletePlan(c.Request.Context(), center.ID, id); err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// --- Administración de socios ---

// ListMembers lista los socios del centro con el nombre de su plan.
func (h *MembershipHandler) ListMembers(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	members, err := h.membership.ListMembers(c.Request.Context(), center.ID)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"members": members})
}

// CreateMember registra un socio.
func (h *MembershipHandler) CreateMember(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	var member domain.Member
	if err := c.ShouldBindJSON(&member); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "datos del socio inválidos"})
		return
	}
	created, err := h.membership.CreateMember(c.Request.Context(), center.ID, &member, h.baseHandler.GetUserID(c))
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, created)
}

// UpdateMember actualiza un socio.
func (h *MembershipHandler) UpdateMember(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	id, err := parseObjectID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return
	}
	var member domain.Member
	if err := c.ShouldBindJSON(&member); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "datos del socio inválidos"})
		return
	}
	updated, err := h.membership.UpdateMember(c.Request.Context(), center.ID, id, &member)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// DeleteMember elimina un socio.
func (h *MembershipHandler) DeleteMember(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	id, err := parseObjectID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return
	}
	if err := h.membership.DeleteMember(c.Request.Context(), center.ID, id); err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// --- helpers ---

func (h *MembershipHandler) adminCenter(c *gin.Context) (*domain.SportCenter, bool) {
	userID := h.baseHandler.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "usuario no autenticado"})
		return nil, false
	}
	center, err := h.membership.ResolveCenter(c.Request.Context(), userID, c.Query("center_id"))
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return nil, false
	}
	return center, true
}

func (h *MembershipHandler) resolvePublicCenter(c *gin.Context, centerID, slug string) (*domain.SportCenter, error) {
	if strings.TrimSpace(centerID) != "" {
		id, err := parseObjectID(centerID)
		if err != nil {
			return nil, domain.NewBadRequestError("center_id inválido")
		}
		center, err := h.centerRepo.FindByID(c.Request.Context(), id)
		if err != nil || center == nil {
			return nil, domain.NewNotFoundError("centro deportivo no encontrado")
		}
		return center, nil
	}
	if strings.TrimSpace(slug) != "" {
		center, err := h.centerRepo.FindBySlug(c.Request.Context(), strings.TrimSpace(slug))
		if err != nil || center == nil {
			return nil, domain.NewNotFoundError("centro deportivo no encontrado")
		}
		return center, nil
	}
	return nil, domain.NewBadRequestError("debes indicar el centro deportivo")
}

func parseObjectID(value string) (primitive.ObjectID, error) {
	id, err := primitive.ObjectIDFromHex(strings.TrimSpace(value))
	if err != nil {
		return primitive.NilObjectID, err
	}
	return id, nil
}
