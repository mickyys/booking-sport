package infra

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/internal/infra/mongo"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mongodb "go.mongodb.org/mongo-driver/mongo"
)

type SuperAdminHandler struct {
	repo *mongo.SuperAdminRepository
}

func NewSuperAdminHandler(repo *mongo.SuperAdminRepository) *SuperAdminHandler {
	return &SuperAdminHandler{repo: repo}
}

func pageParams(c *gin.Context, defLimit, maxLimit int) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(defLimit)))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > maxLimit {
		limit = defLimit
	}
	return page, limit
}

// Audit registra la acción tras un handler exitoso (status < 400).
func (h *SuperAdminHandler) Audit(action, entityType, idParam string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if c.Writer.Status() >= 400 {
			return
		}
		h.writeAudit(c, action, entityType, c.Param(idParam), "", nil)
	}
}

func (h *SuperAdminHandler) writeAudit(c *gin.Context, action, entityType, entityID, reason string, details map[string]interface{}) {
	actor, _ := c.Get("user_id")
	email, _ := c.Get("user_email")
	actorID, _ := actor.(string)
	actorEmail, _ := email.(string)
	_ = h.repo.WriteAudit(c.Request.Context(), &mongo.AuditLog{
		ActorID: actorID, ActorEmail: actorEmail, Action: action,
		EntityType: entityType, EntityID: entityID, Reason: reason,
		Details: details, IP: c.ClientIP(),
	})
}

func (h *SuperAdminHandler) ListCenters(c *gin.Context) {
	page, limit := pageParams(c, 20, 100)
	f := mongo.SuperAdminCenterFilter{
		Query:       strings.TrimSpace(c.Query("q")),
		Status:      c.Query("status"),
		PaymentMode: c.Query("payment_mode"),
		City:        strings.TrimSpace(c.Query("city")),
	}
	if v := c.Query("featured"); v != "" {
		b := v == "true"
		f.Featured = &b
	}
	items, total, err := h.repo.ListCenters(c.Request.Context(), f, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "limit": limit})
}

func (h *SuperAdminHandler) CenterStats(c *gin.Context) {
	s, err := h.repo.CenterStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, s)
}

func (h *SuperAdminHandler) GetCenter(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	center, err := h.repo.GetCenter(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, mongodb.ErrNoDocuments) {
			c.JSON(http.StatusNotFound, gin.H{"error": "center not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Users es json:"-" en la entidad pública; el panel solo necesita el conteo.
	c.JSON(http.StatusOK, gin.H{"center": center, "admin_count": len(center.Users)})
}

func (h *SuperAdminHandler) SetStatus(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body struct {
		Status string `json:"status" binding:"required"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	switch body.Status {
	case domain.SportCenterStatusActive, domain.SportCenterStatusSuspended, domain.SportCenterStatusBlocked:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "status must be active, suspended or blocked"})
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if body.Status != domain.SportCenterStatusActive && len(reason) < 5 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reason is required (min 5 characters)"})
		return
	}
	actor, _ := c.Get("user_id")
	actorID, _ := actor.(string)
	if err := h.repo.SetStatus(c.Request.Context(), id, body.Status, reason, actorID); err != nil {
		if errors.Is(err, mongodb.ErrNoDocuments) {
			c.JSON(http.StatusNotFound, gin.H{"error": "center not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.writeAudit(c, "center.status", "sport_center", id.Hex(), reason, map[string]interface{}{"status": body.Status})
	c.JSON(http.StatusOK, gin.H{"status": body.Status})
}

func (h *SuperAdminHandler) SetFeatured(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var body struct {
		IsFeatured bool       `json:"is_featured"`
		Priority   int        `json:"priority"`
		Until      *time.Time `json:"until"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Priority < 0 || body.Priority > 1000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "priority must be between 0 and 1000"})
		return
	}
	if err := h.repo.SetFeatured(c.Request.Context(), id, body.IsFeatured, body.Priority, body.Until); err != nil {
		if errors.Is(err, mongodb.ErrNoDocuments) {
			c.JSON(http.StatusNotFound, gin.H{"error": "center not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.writeAudit(c, "center.featured", "sport_center", id.Hex(), "", map[string]interface{}{
		"is_featured": body.IsFeatured, "priority": body.Priority,
	})
	c.JSON(http.StatusOK, gin.H{"is_featured": body.IsFeatured, "priority": body.Priority})
}

func bookingFilterFromQuery(c *gin.Context) (mongo.SuperAdminBookingFilter, error) {
	f := mongo.SuperAdminBookingFilter{
		Status: c.Query("status"),
		Query:  strings.TrimSpace(c.Query("q")),
	}
	if v := c.Query("center_id"); v != "" {
		id, err := primitive.ObjectIDFromHex(v)
		if err != nil {
			return f, errors.New("invalid center_id")
		}
		f.CenterID = &id
	}
	parse := func(key string) (*time.Time, error) {
		v := c.Query(key)
		if v == "" {
			return nil, nil
		}
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return nil, fmt.Errorf("invalid %s (expected YYYY-MM-DD)", key)
		}
		return &t, nil
	}
	var err error
	if f.From, err = parse("from"); err != nil {
		return f, err
	}
	if f.To, err = parse("to"); err != nil {
		return f, err
	}
	return f, nil
}

func (h *SuperAdminHandler) ListBookings(c *gin.Context) {
	page, limit := pageParams(c, 25, 100)
	f, err := bookingFilterFromQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items, total, err := h.repo.ListBookings(c.Request.Context(), f, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "limit": limit})
}

func (h *SuperAdminHandler) ExportBookings(c *gin.Context) {
	f, err := bookingFilterFromQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items, _, err := h.repo.ListBookings(c.Request.Context(), f, 1, 5000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="reservas.csv"`)
	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"codigo", "centro", "cancha", "fecha", "hora", "estado", "metodo_pago", "precio_final", "pagado", "pendiente", "cliente"})
	for _, b := range items {
		client := b.UserID
		if b.GuestDetails != nil {
			client = b.GuestDetails.Email
		}
		date := b.LocalDate
		if date == "" {
			date = b.Date.Format("2006-01-02")
		}
		_ = w.Write([]string{
			csvSafe(b.BookingCode), csvSafe(b.SportCenterName), csvSafe(b.CourtName), date,
			fmt.Sprintf("%02d:%02d", b.Hour, b.Minutes), string(b.Status), csvSafe(b.PaymentMethod),
			strconv.FormatFloat(b.ChargedPrice(), 'f', 0, 64), strconv.FormatFloat(b.PaidAmount, 'f', 0, 64),
			strconv.FormatFloat(b.PendingAmount, 'f', 0, 64), csvSafe(client),
		})
	}
	w.Flush()
}

// csvSafe neutraliza inyección de fórmulas al abrir el CSV en hojas de cálculo.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func (h *SuperAdminHandler) ListAudit(c *gin.Context) {
	page, limit := pageParams(c, 30, 100)
	items, total, err := h.repo.ListAudit(c.Request.Context(), c.Query("entity_id"), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "limit": limit})
}
