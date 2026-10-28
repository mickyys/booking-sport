package infra

import (
	"net/http"
	"net/url"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/pkg/logger"
)

type MPConnectHandler struct {
	useCase     *app.MPConnectUseCase
	baseHandler *BaseHandler
}

func NewMPConnectHandler(uc *app.MPConnectUseCase) *MPConnectHandler {
	return &MPConnectHandler{
		useCase:     uc,
		baseHandler: NewBaseHandler(),
	}
}

// GetAuthorizationURL devuelve la URL de autorización OAuth de Mercado Pago
// para conectar la cuenta del centro deportivo del usuario autenticado.
func (h *MPConnectHandler) GetAuthorizationURL(c *gin.Context) {
	userID := h.baseHandler.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found in token"})
		return
	}

	authURL, err := h.useCase.GetAuthorizationURL(c.Request.Context(), userID)
	if err != nil {
		h.baseHandler.LogError(c, "mp_connect_auth_url_error", err, "user_id", userID)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"auth_url": authURL})
}

// Callback recibe el redirect de Mercado Pago luego de que el vendedor autoriza
// la conexión. Es una ruta pública (Mercado Pago no envía token JWT).
func (h *MPConnectHandler) Callback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")

	traceID := logger.GetTraceID(c.Request.Context())
	centerID, _ := h.useCase.DecodeState(state)

	log := h.baseHandler.GetLogger(c)
	log.Infow("mp_connect_callback_received",
		"trace_id", traceID,
		"has_code", code != "",
		"has_state", state != "",
		"center_id", centerID,
		"mp_error", c.Query("error"),
		"mp_error_description", c.Query("error_description"),
	)

	frontendURL := os.Getenv("URL_FRONTEND")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}

	if code == "" {
		log.Warnw("mp_connect_callback_missing_code",
			"trace_id", traceID,
			"center_id", centerID,
			"mp_error", c.Query("error"),
			"mp_error_description", c.Query("error_description"),
		)
		c.Redirect(http.StatusFound, frontendURL+"/admin/onboarding?status=error&reason=missing_code&trace_id="+url.QueryEscape(traceID))
		return
	}

	if _, err := h.useCase.HandleCallback(c.Request.Context(), code, state); err != nil {
		h.baseHandler.LogError(c, "mp_connect_callback_error", err,
			"trace_id", traceID,
			"center_id", centerID,
			"has_code", code != "",
		)
		c.Redirect(http.StatusFound, frontendURL+"/admin/onboarding?status=error&reason=callback_failed&trace_id="+url.QueryEscape(traceID))
		return
	}

	log.Infow("mp_connect_callback_success",
		"trace_id", traceID,
		"center_id", centerID,
	)
	c.Redirect(http.StatusFound, frontendURL+"/admin/onboarding?status=success&trace_id="+url.QueryEscape(traceID))
}

// GetStatus devuelve el estado de conexión de Mercado Pago del centro.
func (h *MPConnectHandler) GetStatus(c *gin.Context) {
	userID := h.baseHandler.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found in token"})
		return
	}

	status, err := h.useCase.GetStatus(c.Request.Context(), userID)
	if err != nil {
		h.baseHandler.LogError(c, "mp_connect_status_error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, status)
}

// Disconnect revoca el acceso de la plataforma a la cuenta del vendedor.
func (h *MPConnectHandler) Disconnect(c *gin.Context) {
	userID := h.baseHandler.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found in token"})
		return
	}

	if err := h.useCase.Disconnect(c.Request.Context(), userID); err != nil {
		h.baseHandler.LogError(c, "mp_connect_disconnect_error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "disconnected"})
}
