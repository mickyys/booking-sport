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

func (h *SportCenterHandler) Create(c *gin.Context) {
	var center domain.SportCenter
	if err := c.ShouldBindJSON(&center); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.useCase.CreateSportCenter(c.Request.Context(), &center); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, center)
}

func (h *SportCenterHandler) Update(c *gin.Context) {
	idStr := c.Param("id")
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	var center domain.SportCenter
	if err := c.ShouldBindJSON(&center); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.useCase.UpdateSportCenter(c.Request.Context(), id, &center); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, center)
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

	var schedule []domain.CourtSchedule
	if err := c.ShouldBindJSON(&schedule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validar horarios (6 AM a 24 PM)
	for _, s := range schedule {
		if s.Hour < 6 || s.Hour > 24 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Hour must be between 6 and 24"})
			return
		}
	}

	if err := h.useCase.ConfigureSchedule(c.Request.Context(), courtID, schedule); err != nil {
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
