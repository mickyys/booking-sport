package infra

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/internal/infra/mongo"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// AccessHandler expone el formulario público y la bandeja del administrador
// para los centros privados.
type AccessHandler struct {
	baseHandler *BaseHandler
	useCase     *app.CenterAccessUseCase
	centerRepo  app.SportCenterRepository
	audit       *mongo.SuperAdminRepository
}

func NewAccessHandler(useCase *app.CenterAccessUseCase, centerRepo app.SportCenterRepository) *AccessHandler {
	return &AccessHandler{
		baseHandler: NewBaseHandler(),
		useCase:     useCase,
		centerRepo:  centerRepo,
	}
}

// SetAuditRepository habilita el registro de aprobaciones y rechazos.
func (h *AccessHandler) SetAuditRepository(repo *mongo.SuperAdminRepository) {
	h.audit = repo
}

type accessRequestInput struct {
	RUT            string            `json:"rut"`
	Email          string            `json:"email"`
	Name           string            `json:"name"`
	Answers        map[string]string `json:"answers"`
	TurnstileToken string            `json:"turnstile_token"`
}

func (in accessRequestInput) toApp() app.CenterAccessRequestInput {
	return app.CenterAccessRequestInput{
		RUT:     in.RUT,
		Email:   in.Email,
		Name:    in.Name,
		Answers: in.Answers,
	}
}

// --- Público ---

// SubmitRequest recibe el formulario de acceso de un centro privado.
func (h *AccessHandler) SubmitRequest(c *gin.Context) {
	center, err := h.publicCenter(c)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	var body accessRequestInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "datos incompletos"})
		return
	}
	if secret := strings.TrimSpace(os.Getenv("TURNSTILE_SECRET_KEY")); secret != "" {
		if !verifyTurnstile(body.TurnstileToken, secret, c.ClientIP()) && os.Getenv("GIN_MODE") == "release" {
			c.JSON(http.StatusForbidden, gin.H{"error": "validación de seguridad fallida"})
			return
		}
	}
	request, err := h.useCase.SubmitRequest(c.Request.Context(), center.ID, body.toApp(), c.ClientIP())
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	h.baseHandler.LogRequest(c, "center_access_request_created",
		"center_id", center.ID.Hex(),
		"request_id", request.ID.Hex(),
	)
	c.JSON(http.StatusCreated, gin.H{
		"request_id": request.ID.Hex(),
		"status":     request.Status,
	})
}

type accessVerifyRequestInput struct {
	RUT string `json:"rut"`
}

// RequestVerification envía el código al correo registrado de una solicitud
// aprobada.
func (h *AccessHandler) RequestVerification(c *gin.Context) {
	center, err := h.publicCenter(c)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	var body accessVerifyRequestInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "datos incompletos"})
		return
	}
	result, err := h.useCase.RequestVerification(c.Request.Context(), center.ID, body.RUT, c.ClientIP())
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

type accessVerifyConfirmInput struct {
	VerificationID string `json:"verification_id"`
	Code           string `json:"code"`
}

// ConfirmVerification valida el código y entrega el token de acceso.
func (h *AccessHandler) ConfirmVerification(c *gin.Context) {
	var body accessVerifyConfirmInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "datos incompletos"})
		return
	}
	result, err := h.useCase.ConfirmVerification(c.Request.Context(), body.VerificationID, body.Code)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

type accessValidateInput struct {
	AccessToken string `json:"access_token"`
}

// ValidateToken permite a la ficha saber si el token guardado sigue vigente.
func (h *AccessHandler) ValidateToken(c *gin.Context) {
	center, err := h.publicCenter(c)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	var body accessValidateInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "datos incompletos"})
		return
	}
	request, err := h.useCase.ValidateToken(c.Request.Context(), center.ID, body.AccessToken)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"valid": true,
		"applicant": gin.H{
			"name":  request.Name,
			"email": request.Email,
			"rut":   request.RUT,
		},
	})
}

// --- Administrador del centro ---

// accessFilterFromQuery arma el filtro de la bandeja a partir del query string.
func accessFilterFromQuery(c *gin.Context, centerID primitive.ObjectID) app.CenterAccessRequestFilter {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	filter := app.CenterAccessRequestFilter{
		CenterID: centerID,
		Status:   strings.TrimSpace(c.Query("status")),
		Query:    strings.TrimSpace(c.Query("q")),
		Page:     page,
		Limit:    limit,
		Sort:     strings.TrimSpace(c.Query("sort")),
		Desc:     c.DefaultQuery("dir", "desc") != "asc",
	}
	loc := domain.GetSantiagoLocation()
	if from, err := time.ParseInLocation("2006-01-02", c.Query("from"), loc); err == nil {
		filter.From = &from
	}
	if to, err := time.ParseInLocation("2006-01-02", c.Query("to"), loc); err == nil {
		end := to.AddDate(0, 0, 1)
		filter.To = &end
	}
	return filter
}

// ListRequests lista las solicitudes del centro.
func (h *AccessHandler) ListRequests(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	filter := accessFilterFromQuery(c, center.ID)
	items, total, err := h.useCase.ListRequests(c.Request.Context(), filter)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	limit := filter.Limit
	if limit < 1 || limit > 100 {
		limit = 20
	}
	page := filter.Page
	if page < 1 {
		page = 1
	}
	totalPages := int((total + int64(limit) - 1) / int64(limit))
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "limit": limit, "total_pages": totalPages})
}

// Stats devuelve los contadores por estado.
func (h *AccessHandler) Stats(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	counts, err := h.useCase.Counts(c.Request.Context(), center.ID)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, counts)
}

// GetRequest devuelve el detalle de una solicitud.
func (h *AccessHandler) GetRequest(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return
	}
	request, err := h.useCase.GetRequest(c.Request.Context(), center.ID, id)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, request)
}

// CreateManual registra a una persona ya aprobada por el administrador.
func (h *AccessHandler) CreateManual(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	var body accessRequestInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "datos incompletos"})
		return
	}
	actorID := h.baseHandler.GetUserID(c)
	actorEmail := c.GetString("user_email")
	request, err := h.useCase.CreateManualRequest(c.Request.Context(), center.ID, actorID, actorEmail, body.toApp())
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	h.writeAudit(c, "center_access.create", center.ID, request.ID, "alta manual", map[string]interface{}{"rut": request.RUT})
	c.JSON(http.StatusCreated, request)
}

type accessStatusInput struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// SetRequestStatus aprueba, rechaza o revoca una solicitud.
func (h *AccessHandler) SetRequestStatus(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return
	}
	var body accessStatusInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "datos incompletos"})
		return
	}
	actorID := h.baseHandler.GetUserID(c)
	actorEmail := c.GetString("user_email")
	request, err := h.useCase.SetRequestStatus(c.Request.Context(), center.ID, id, body.Status, body.Reason, actorID, actorEmail)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	action := "center_access." + request.Status
	h.writeAudit(c, action, center.ID, request.ID, request.Reason, map[string]interface{}{"status": request.Status})
	c.JSON(http.StatusOK, request)
}

// GetForm devuelve la configuración del formulario y si el centro es privado.
func (h *AccessHandler) GetForm(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	form, err := h.useCase.GetForm(c.Request.Context(), center.ID)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"form": form, "is_private": center.IsPrivate})
}

// UpdateForm guarda la configuración del formulario.
func (h *AccessHandler) UpdateForm(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	var body domain.CenterAccessForm
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	form, err := h.useCase.UpdateForm(c.Request.Context(), center.ID, h.baseHandler.GetUserID(c), &body)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, form)
}

// --- helpers ---

func (h *AccessHandler) publicCenter(c *gin.Context) (*domain.SportCenter, error) {
	slug := strings.TrimSpace(c.Param("slug"))
	center, err := h.centerRepo.FindBySlug(c.Request.Context(), slug)
	if err != nil || center == nil || !center.IsOperational() {
		return nil, domain.NewNotFoundError("centro deportivo no encontrado")
	}
	return center, nil
}

func (h *AccessHandler) adminCenter(c *gin.Context) (*domain.SportCenter, bool) {
	userID := h.baseHandler.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "usuario no autenticado"})
		return nil, false
	}
	centers, err := h.centerRepo.FindByUserID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return nil, false
	}
	if len(centers) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no tienes un centro asignado"})
		return nil, false
	}
	if raw := strings.TrimSpace(c.Query("center_id")); raw != "" {
		id, err := primitive.ObjectIDFromHex(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "center_id inválido"})
			return nil, false
		}
		for i := range centers {
			if centers[i].ID == id {
				return &centers[i], true
			}
		}
		c.JSON(http.StatusForbidden, gin.H{"error": "no tienes acceso a este centro"})
		return nil, false
	}
	return &centers[0], true
}

func (h *AccessHandler) writeAudit(c *gin.Context, action string, centerID, requestID primitive.ObjectID, reason string, details map[string]interface{}) {
	if h.audit == nil {
		return
	}
	actorID := h.baseHandler.GetUserID(c)
	actorEmail := c.GetString("user_email")
	if details == nil {
		details = map[string]interface{}{}
	}
	details["center_id"] = centerID.Hex()
	_ = h.audit.WriteAudit(c.Request.Context(), &mongo.AuditLog{
		ActorID: actorID, ActorEmail: actorEmail, Action: action,
		EntityType: "center_access_request", EntityID: requestID.Hex(),
		Reason: reason, Details: details, IP: c.ClientIP(), CreatedAt: time.Now(),
	})
}
