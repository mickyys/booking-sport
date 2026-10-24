package infra

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ScheduleHandler struct {
	useCase *app.ScheduleUseCase
}

func NewScheduleHandler(useCase *app.ScheduleUseCase) *ScheduleHandler {
	return &ScheduleHandler{useCase: useCase}
}

func userIDFromContext(c *gin.Context) (string, bool) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return "", false
	}
	uid, ok := userID.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid User ID type"})
		return "", false
	}
	return uid, true
}

func spaceIDFromParam(c *gin.Context) (primitive.ObjectID, bool) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid space ID format"})
		return primitive.NilObjectID, false
	}
	return id, true
}

func objectIDFromParam(c *gin.Context, name string) (primitive.ObjectID, bool) {
	id, err := primitive.ObjectIDFromHex(c.Param(name))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid " + name + " format"})
		return primitive.NilObjectID, false
	}
	return id, true
}

// GetSchedule retorna las reglas semanales de un espacio.
func (h *ScheduleHandler) GetSchedule(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	spaceID, ok := spaceIDFromParam(c)
	if !ok {
		return
	}
	rules, source, err := h.useCase.GetScheduleConfig(c.Request.Context(), spaceID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rules": rules, "source": source})
}

// SaveSchedule reemplaza las reglas semanales de un espacio (PUT).
func (h *ScheduleHandler) SaveSchedule(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	spaceID, ok := spaceIDFromParam(c)
	if !ok {
		return
	}

	var body struct {
		Rules []domain.ScheduleRule `json:"rules"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rules, err := h.useCase.SaveSchedule(c.Request.Context(), spaceID, userID, body.Rules)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rules": rules})
}

// ListExceptions retorna las excepciones de horario de un espacio.
func (h *ScheduleHandler) ListExceptions(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	spaceID, ok := spaceIDFromParam(c)
	if !ok {
		return
	}
	exceptions, err := h.useCase.ListExceptions(c.Request.Context(), spaceID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"exceptions": exceptions})
}

// CreateException crea una excepción de horario.
func (h *ScheduleHandler) CreateException(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	spaceID, ok := spaceIDFromParam(c)
	if !ok {
		return
	}

	var body struct {
		Type  string                    `json:"type"`
		Date  string                    `json:"date"`
		Slots []domain.ScheduleRuleSlot `json:"slots"`
		Note  string                    `json:"note"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	parsed, err := parseSantiagoDate(body.Date)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, expected YYYY-MM-DD"})
		return
	}

	ex := &domain.ScheduleException{Type: body.Type, Date: parsed, Slots: body.Slots, Note: body.Note}
	created, err := h.useCase.CreateException(c.Request.Context(), spaceID, userID, ex)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"exception": created})
}

// UpdateException actualiza una excepción de horario.
func (h *ScheduleHandler) UpdateException(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	spaceID, ok := spaceIDFromParam(c)
	if !ok {
		return
	}
	exID, ok := objectIDFromParam(c, "exceptionId")
	if !ok {
		return
	}

	var body struct {
		Type  string                    `json:"type"`
		Date  string                    `json:"date"`
		Slots []domain.ScheduleRuleSlot `json:"slots"`
		Note  string                    `json:"note"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	parsed, err := parseSantiagoDate(body.Date)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, expected YYYY-MM-DD"})
		return
	}

	ex := &domain.ScheduleException{Type: body.Type, Date: parsed, Slots: body.Slots, Note: body.Note}
	updated, err := h.useCase.UpdateException(c.Request.Context(), spaceID, exID, userID, ex)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"exception": updated})
}

// DeleteException elimina una excepción de horario.
func (h *ScheduleHandler) DeleteException(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	spaceID, ok := spaceIDFromParam(c)
	if !ok {
		return
	}
	exID, ok := objectIDFromParam(c, "exceptionId")
	if !ok {
		return
	}

	if err := h.useCase.DeleteException(c.Request.Context(), spaceID, exID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Exception deleted"})
}

// ListBlocks retorna los bloqueos de un espacio.
func (h *ScheduleHandler) ListBlocks(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	spaceID, ok := spaceIDFromParam(c)
	if !ok {
		return
	}
	blocks, err := h.useCase.ListBlocks(c.Request.Context(), spaceID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"blocks": blocks})
}

// CreateBlock crea un bloqueo puntual de agenda.
func (h *ScheduleHandler) CreateBlock(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	spaceID, ok := spaceIDFromParam(c)
	if !ok {
		return
	}

	var body struct {
		Date      string `json:"date"`
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
		Reason    string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	parsed, err := parseSantiagoDate(body.Date)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, expected YYYY-MM-DD"})
		return
	}

	block := &domain.Block{Date: parsed, StartTime: body.StartTime, EndTime: body.EndTime, Reason: body.Reason}
	created, err := h.useCase.CreateBlock(c.Request.Context(), spaceID, userID, block)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"block": created})
}

// DeleteBlock elimina un bloqueo puntual.
func (h *ScheduleHandler) DeleteBlock(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	spaceID, ok := spaceIDFromParam(c)
	if !ok {
		return
	}
	blockID, ok := objectIDFromParam(c, "blockId")
	if !ok {
		return
	}

	if err := h.useCase.DeleteBlock(c.Request.Context(), spaceID, blockID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Block deleted"})
}

// parseSantiagoDate convierte "YYYY-MM-DD" a medianoche de Chile.
func parseSantiagoDate(date string) (time.Time, error) {
	loc := domain.GetSantiagoLocation()
	parsed, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, loc), nil
}

// PublicAvailability retorna la disponibilidad pública de un espacio para una
// fecha o rango de fechas (sin autenticación). Usado por facilities.
func (h *ScheduleHandler) PublicAvailability(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid space ID format"})
		return
	}

	loc := domain.GetSantiagoLocation()

	// Rango de fechas: ?from=YYYY-MM-DD&to=YYYY-MM-DD
	fromStr := c.Query("from")
	toStr := c.Query("to")
	if fromStr != "" && toStr != "" {
		from, errFrom := time.ParseInLocation("2006-01-02", fromStr, loc)
		to, errTo := time.ParseInLocation("2006-01-02", toStr, loc)
		if errFrom != nil || errTo != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid from/to date format, expected YYYY-MM-DD"})
			return
		}
		resp, err := h.useCase.PublicAvailabilityRange(c.Request.Context(), id, from, to)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": resp})
		return
	}

	date := time.Now().In(loc)
	if dateStr := c.Query("date"); dateStr != "" {
		parsed, err := time.ParseInLocation("2006-01-02", dateStr, loc)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, expected YYYY-MM-DD"})
			return
		}
		date = parsed
	}

	resp, err := h.useCase.PublicAvailability(c.Request.Context(), id, date)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": resp})
}
