package infra

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
)

// DiscountHandler expone la generación y administración de códigos de
// descuento del centro activo del organizador.
type DiscountHandler struct {
	baseHandler *BaseHandler
	useCase     *app.DiscountUseCase
	membership  *app.MembershipUseCase
}

func NewDiscountHandler(useCase *app.DiscountUseCase, membership *app.MembershipUseCase) *DiscountHandler {
	return &DiscountHandler{
		baseHandler: NewBaseHandler(),
		useCase:     useCase,
		membership:  membership,
	}
}

type discountCodeRequest struct {
	Code       string               `json:"code"`
	Discount   domain.DiscountValue `json:"discount"`
	Scope      domain.DiscountScope `json:"scope"`
	UsageLimit domain.UsageLimit    `json:"usage_limit"`
	MinAmount  float64              `json:"min_amount"`
	ValidFrom  *time.Time           `json:"valid_from"`
	ValidTo    *time.Time           `json:"valid_to"`
}

type discountBatchRequest struct {
	Count      int                  `json:"count" binding:"required"`
	Prefix     string               `json:"prefix"`
	Discount   domain.DiscountValue `json:"discount"`
	Scope      domain.DiscountScope `json:"scope"`
	UsageLimit domain.UsageLimit    `json:"usage_limit"`
	MinAmount  float64              `json:"min_amount"`
	ValidFrom  *time.Time           `json:"valid_from"`
	ValidTo    *time.Time           `json:"valid_to"`
}

// ListCodes devuelve los códigos del centro con su estado derivado.
func (h *DiscountHandler) ListCodes(c *gin.Context) {
	center, ok := h.center(c)
	if !ok {
		return
	}
	codes, err := h.useCase.ListCodes(c.Request.Context(), center.ID)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"codes": codes})
}

// CreateCode crea un único código.
func (h *DiscountHandler) CreateCode(c *gin.Context) {
	center, ok := h.center(c)
	if !ok {
		return
	}
	var input discountCodeRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "datos del código inválidos"})
		return
	}
	code, err := h.useCase.CreateCode(c.Request.Context(), center.ID, app.CreateCodeInput{
		Code:       input.Code,
		Discount:   input.Discount,
		Scope:      input.Scope,
		UsageLimit: input.UsageLimit,
		MinAmount:  input.MinAmount,
		ValidFrom:  input.ValidFrom,
		ValidTo:    input.ValidTo,
	}, h.baseHandler.GetUserID(c))
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, code)
}

// CreateBatch genera un lote de códigos.
func (h *DiscountHandler) CreateBatch(c *gin.Context) {
	center, ok := h.center(c)
	if !ok {
		return
	}
	var input discountBatchRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "datos del lote inválidos"})
		return
	}
	codes, err := h.useCase.CreateBatch(c.Request.Context(), center.ID, app.BatchCodeInput{
		Count:      input.Count,
		Prefix:     input.Prefix,
		Discount:   input.Discount,
		Scope:      input.Scope,
		UsageLimit: input.UsageLimit,
		MinAmount:  input.MinAmount,
		ValidFrom:  input.ValidFrom,
		ValidTo:    input.ValidTo,
	}, h.baseHandler.GetUserID(c))
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"codes": codes, "count": len(codes)})
}

// VoidCode anula un código.
func (h *DiscountHandler) VoidCode(c *gin.Context) {
	center, ok := h.center(c)
	if !ok {
		return
	}
	id, err := parseObjectID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return
	}
	if err := h.useCase.VoidCode(c.Request.Context(), center.ID, id); err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// VoidBatch anula todos los códigos de un lote.
func (h *DiscountHandler) VoidBatch(c *gin.Context) {
	center, ok := h.center(c)
	if !ok {
		return
	}
	if err := h.useCase.VoidBatch(c.Request.Context(), center.ID, c.Param("batchId")); err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ListUsages devuelve quién usó los códigos del centro.
func (h *DiscountHandler) ListUsages(c *gin.Context) {
	center, ok := h.center(c)
	if !ok {
		return
	}
	limit := 200
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}
	usages, err := h.useCase.ListUsages(c.Request.Context(), center.ID, c.Query("code_id"), limit)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"usages": usages})
}

// ExportCodes descarga los códigos del centro en CSV.
func (h *DiscountHandler) ExportCodes(c *gin.Context) {
	center, ok := h.center(c)
	if !ok {
		return
	}
	codes, err := h.useCase.ListCodes(c.Request.Context(), center.ID)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	_ = writer.Write([]string{
		"codigo", "lote", "tipo", "valor", "alcance", "monto_minimo",
		"max_usos", "usos", "max_usos_por_usuario", "periodo",
		"vigente_desde", "vigente_hasta", "estado", "estado_derivado", "creado",
	})
	for _, code := range codes {
		_ = writer.Write([]string{
			code.Code,
			code.BatchID,
			string(code.Discount.Type),
			strconv.FormatFloat(code.Discount.Value, 'f', -1, 64),
			string(code.Scope.Mode),
			strconv.FormatFloat(code.MinAmount, 'f', -1, 64),
			strconv.Itoa(code.UsageLimit.MaxUses),
			strconv.Itoa(code.UsedCount),
			strconv.Itoa(code.UsageLimit.MaxUsesPerUser),
			string(code.UsageLimit.Period),
			formatOptionalTime(code.ValidFrom),
			formatOptionalTime(code.ValidTo),
			string(code.Status),
			code.DerivedStatus,
			code.CreatedAt.Format(time.RFC3339),
		})
	}
	writer.Flush()

	filename := "codigos-" + center.Slug + "-" + time.Now().Format("20060102") + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.String(http.StatusOK, buf.String())
}

func (h *DiscountHandler) center(c *gin.Context) (*domain.SportCenter, bool) {
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

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

// normalizeCodeParam se usa en rutas donde el código viene en la URL.
func normalizeCodeParam(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}
