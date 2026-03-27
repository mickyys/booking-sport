package infra

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type SportCenterHandler struct {
	useCase *app.SportCenterUseCase
}

func NewSportCenterHandler(uc *app.SportCenterUseCase) *SportCenterHandler {
	return &SportCenterHandler{useCase: uc}
}

func (h *SportCenterHandler) List(c *gin.Context) {
	pageStr := c.Query("page")
	limitStr := c.Query("limit")

	page, _ := strconv.Atoi(pageStr)
	limit, _ := strconv.Atoi(limitStr)

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}

	response, err := h.useCase.ListSportCentersPaged(c.Request.Context(), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *SportCenterHandler) GetBySlug(c *gin.Context) {
	slug := c.Param("slug")
	center, err := h.useCase.FindBySlug(c.Request.Context(), slug)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Sport center not found"})
		return
	}
	cancellationPolicy := gin.H{
		"hours":             center.CancellationHours,
		"retention_percent": center.RetentionPercent,
	}

	c.JSON(http.StatusOK, gin.H{"center": center, "cancellation_policy": cancellationPolicy})
}

func (h *SportCenterHandler) Create(c *gin.Context) {
	var body struct {
		domain.SportCenter
		Fintoc *domain.FintocConfig `json:"fintoc"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	center := body.SportCenter
	center.Fintoc = body.Fintoc

	if err := h.useCase.CreateSportCenter(c.Request.Context(), &center); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	cancellationPolicy := gin.H{
		"hours":             center.CancellationHours,
		"retention_percent": center.RetentionPercent,
	}

	c.JSON(http.StatusCreated, gin.H{"center": center, "cancellation_policy": cancellationPolicy})
}

func (h *SportCenterHandler) Update(c *gin.Context) {
	idStr := c.Param("id")
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	var body struct {
		domain.SportCenter
		Fintoc *domain.FintocConfig `json:"fintoc"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	center := body.SportCenter
	center.Fintoc = body.Fintoc

	if err := h.useCase.UpdateSportCenter(c.Request.Context(), id, &center); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	cancellationPolicy := gin.H{
		"hours":             center.CancellationHours,
		"retention_percent": center.RetentionPercent,
	}

	c.JSON(http.StatusOK, gin.H{"center": center, "cancellation_policy": cancellationPolicy})
}

type CourtHandler struct {
	useCase *app.CourtUseCase
}

func NewCourtHandler(uc *app.CourtUseCase) *CourtHandler {
	return &CourtHandler{useCase: uc}
}

func (h *CourtHandler) List(c *gin.Context) {
	pageStr := c.Query("page")
	limitStr := c.Query("limit")

	page, _ := strconv.Atoi(pageStr)
	limit, _ := strconv.Atoi(limitStr)

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}

	response, err := h.useCase.ListCourtsPaged(c.Request.Context(), page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *CourtHandler) CreateCourt(c *gin.Context) {
	var body struct {
		SportCenterID primitive.ObjectID `json:"sport_center_id"`
		Name          string             `json:"name"`
		Description   string             `json:"description"`
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	court := &domain.Court{
		SportCenterID: body.SportCenterID,
		Name:          body.Name,
		Description:   body.Description,
	}

	if err := h.useCase.CreateCourt(c.Request.Context(), court); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, court)
}

func (h *CourtHandler) ConfigureSchedule(c *gin.Context) {
	idStr := c.Param("id")
	courtID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid court ID format"})
		return
	}

	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return
	}

	userIDStr, ok := userID.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid User ID type"})
		return
	}

	var schedule []domain.CourtSchedule
	if err := c.ShouldBindJSON(&schedule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validar horarios (0 a 23 y 0 a 59)
	for _, s := range schedule {
		if s.Hour < 0 || s.Hour > 23 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Hour must be between 0 and 23"})
			return
		}
		if s.Minutes < 0 || s.Minutes > 59 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Minutes must be between 0 and 59"})
			return
		}
	}

	if err := h.useCase.ConfigureSchedule(c.Request.Context(), courtID, schedule, userIDStr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

func (h *CourtHandler) GetSchedule(c *gin.Context) {
	idStr := c.Param("id")
	courtID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid court ID format"})
		return
	}

	dateStr := c.Query("date")
	date := time.Now()
	if dateStr != "" {
		parsedDate, err := time.Parse("2006-01-02", dateStr)
		if err == nil {
			date = parsedDate
		}
	}

	all := c.Query("all") == "true"
	schedule, err := h.useCase.GetCourtSchedule(c.Request.Context(), courtID, date, all)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, schedule)
}

func (h *CourtHandler) GetAdminCourts(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return
	}

	userIDStr, ok := userID.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid User ID type"})
		return
	}

	response, err := h.useCase.GetCourtsByAdminUser(c.Request.Context(), userIDStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *CourtHandler) CreateAdminCourt(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return
	}

	userIDStr, ok := userID.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid User ID type"})
		return
	}

	var body struct {
		SportCenterID primitive.ObjectID `json:"sport_center_id"`
		Name          string             `json:"name"`
		Description   string             `json:"description"`
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	court := &domain.Court{
		SportCenterID: body.SportCenterID,
		Name:          body.Name,
		Description:   body.Description,
	}

	if err := h.useCase.CreateAdminCourt(c.Request.Context(), court, userIDStr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, court)
}

func (h *CourtHandler) UpdateAdminCourt(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return
	}

	userIDStr, ok := userID.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid User ID type"})
		return
	}

	idStr := c.Param("id")
	courtID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid court ID format"})
		return
	}

	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updatedCourt := &domain.Court{
		Name:        body.Name,
		Description: body.Description,
	}

	if err := h.useCase.UpdateAdminCourt(c.Request.Context(), courtID, updatedCourt, userIDStr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Court updated successfully"})
}

func (h *CourtHandler) DeleteAdminCourt(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return
	}

	userIDStr, ok := userID.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid User ID type"})
		return
	}

	idStr := c.Param("id")
	courtID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid court ID format"})
		return
	}

	if err := h.useCase.DeleteAdminCourt(c.Request.Context(), courtID, userIDStr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Court deleted successfully"})
}

func (h *SportCenterHandler) GetSchedules(c *gin.Context) {
	idStr := c.Param("id")
	centerID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid sport center ID format"})
		return
	}

	dateStr := c.Query("date")
	date := time.Now()
	if dateStr != "" {
		parsedDate, err := time.Parse("2006-01-02", dateStr)
		if err == nil {
			date = parsedDate
		}
	}

	all := c.Query("all") == "true"
	schedules, err := h.useCase.GetSportCenterSchedules(c.Request.Context(), centerID, date, all)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, schedules)
}

type UserHandler struct {
	useCase *app.UserUseCase
}

func NewUserHandler(uc *app.UserUseCase) *UserHandler {
	return &UserHandler{useCase: uc}
}
