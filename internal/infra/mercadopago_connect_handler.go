package infra

import (
	"html"
	"net/http"
	"net/url"
	"os"
	"strings"

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

	authURL, err := h.useCase.GetAuthorizationURL(c.Request.Context(), userID, c.Query("source"))
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
	fromApp := h.useCase.DecodeStateSource(state) == app.MPConnectSourceApp

	if code == "" {
		log.Warnw("mp_connect_callback_missing_code",
			"trace_id", traceID,
			"center_id", centerID,
			"mp_error", c.Query("error"),
			"mp_error_description", c.Query("error_description"),
		)
		if fromApp {
			h.returnToApp(c, "error", "missing_code", traceID)
			return
		}
		c.Redirect(http.StatusFound, frontendURL+"/admin/onboarding?status=error&reason=missing_code&trace_id="+url.QueryEscape(traceID))
		return
	}

	if _, err := h.useCase.HandleCallback(c.Request.Context(), code, state); err != nil {
		h.baseHandler.LogError(c, "mp_connect_callback_error", err,
			"trace_id", traceID,
			"center_id", centerID,
			"has_code", code != "",
		)
		if fromApp {
			h.returnToApp(c, "error", "callback_failed", traceID)
			return
		}
		c.Redirect(http.StatusFound, frontendURL+"/admin/onboarding?status=error&reason=callback_failed&trace_id="+url.QueryEscape(traceID))
		return
	}

	log.Infow("mp_connect_callback_success",
		"trace_id", traceID,
		"center_id", centerID,
	)
	if fromApp {
		h.returnToApp(c, "success", "", traceID)
		return
	}
	c.Redirect(http.StatusFound, frontendURL+"/admin/onboarding?status=success&trace_id="+url.QueryEscape(traceID))
}

// mobileAdminScheme es el esquema de la app de administración (el mismo que
// usa Auth0 para volver a la app).
func mobileAdminScheme() string {
	if s := strings.TrimSpace(os.Getenv("MOBILE_ADMIN_SCHEME")); s != "" {
		return s
	}
	return "reservaloyaadmin"
}

// returnToApp devuelve al usuario a la app mobile con un deep link. Responde
// una página mínima con redirección automática y un botón, porque algunos
// navegadores móviles bloquean el redirect directo a un esquema propio.
func (h *MPConnectHandler) returnToApp(c *gin.Context, status, reason, traceID string) {
	q := url.Values{}
	q.Set("status", status)
	if reason != "" {
		q.Set("reason", reason)
	}
	q.Set("trace_id", traceID)
	deepLink := mobileAdminScheme() + "://mp-connect?" + q.Encode()

	title := "Mercado Pago conectado"
	message := "Ya puedes volver a la app de ReservaloYA."
	if status != "success" {
		title = "No se pudo conectar Mercado Pago"
		message = "Vuelve a la app e inténtalo nuevamente."
	}
	page := `<!doctype html><html lang="es"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<meta http-equiv="refresh" content="0;url=` + html.EscapeString(deepLink) + `">` +
		`<title>` + html.EscapeString(title) + `</title>` +
		`<style>body{font-family:system-ui,sans-serif;display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0;background:#f8fafc;color:#0f172a}` +
		`main{max-width:22rem;padding:2rem;text-align:center}a{display:inline-block;margin-top:1rem;padding:.8rem 1.4rem;border-radius:.6rem;background:#047857;color:#fff;text-decoration:none;font-weight:600}</style>` +
		`</head><body><main><h1>` + html.EscapeString(title) + `</h1><p>` + html.EscapeString(message) + `</p>` +
		`<a href="` + html.EscapeString(deepLink) + `">Volver a la app</a></main></body></html>`
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
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
