package infra

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type FacilityHandler struct {
	useCase *app.FacilityUseCase
}

func NewFacilityHandler(useCase *app.FacilityUseCase) *FacilityHandler {
	return &FacilityHandler{useCase: useCase}
}

// GetByCenterID lista las facilities públicas de un centro por id.
func (h *FacilityHandler) GetByCenterID(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid center ID format"})
		return
	}
	facilities, err := h.useCase.ListByCenterID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"facilities": facilities})
}

// GetBySlug lista las facilities públicas de un centro por slug.
func (h *FacilityHandler) GetBySlug(c *gin.Context) {
	facilities, err := h.useCase.ListBySlug(c.Request.Context(), c.Param("slug"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"facilities": facilities})
}

// GetAdmin lista las facilities del centro del admin autenticado.
func (h *FacilityHandler) GetAdmin(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return
	}
	facilities, err := h.useCase.ListByUser(c.Request.Context(), userID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"facilities": facilities})
}

// Create crea una facility para el centro del admin.
func (h *FacilityHandler) Create(c *gin.Context) {
	var body struct {
		SportCenterID string `json:"sport_center_id"`
		domain.Facility
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	centerID, err := primitive.ObjectIDFromHex(body.SportCenterID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid sport_center_id format"})
		return
	}
	facility := body.Facility
	facility.SportCenterID = centerID
	facility.Enabled = true

	if err := h.useCase.Create(c.Request.Context(), &facility); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"facility": facility})
}

// Update actualiza una facility por id.
func (h *FacilityHandler) Update(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid facility ID format"})
		return
	}
	var body struct {
		domain.Facility
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	facility := body.Facility
	facility.ID = id

	existing, err := h.useCase.FindByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Facility not found"})
		return
	}
	facility.SportCenterID = existing.SportCenterID

	if err := h.useCase.Update(c.Request.Context(), &facility); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"facility": facility})
}

// Delete elimina una facility por id.
func (h *FacilityHandler) Delete(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid facility ID format"})
		return
	}
	if err := h.useCase.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Facility deleted"})
}

// CreateInquiry registra una solicitud de arriendo (público).
func (h *FacilityHandler) CreateInquiry(c *gin.Context) {
	var inquiry domain.FacilityInquiry
	if err := c.ShouldBindJSON(&inquiry); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if inquiry.FacilityID.IsZero() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "facility_id is required"})
		return
	}
	if err := h.useCase.CreateInquiry(c.Request.Context(), &inquiry); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Inquiry created", "inquiry": inquiry})
}
