package infra

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/pkg/logger"
)

type ContactHandler struct {
	mailer app.Mailer
	baseHandler *BaseHandler
}

func NewContactHandler(mailer app.Mailer) *ContactHandler {
	return &ContactHandler{
		mailer: mailer,
		baseHandler: NewBaseHandler(),
	}
}

type ContactRequest struct {
	Name            string `json:"name" binding:"required"`
	Email           string `json:"email" binding:"required,email"`
	Phone           string `json:"phone" binding:"required"`
	SportCenterName string `json:"sportCenterName" binding:"required"`
	Message         string `json:"message" binding:"required"`
	TurnstileToken  string `json:"turnstileToken" binding:"required"`
}

type TurnstileResponse struct {
	Success     bool      `json:"success"`
	ChallengeTS time.Time `json:"challenge_ts"`
	Hostname    string    `json:"hostname"`
	ErrorCodes  []string  `json:"error-codes"`
}

func (h *ContactHandler) Submit(c *gin.Context) {
	log := h.baseHandler.GetLogger(c)
	
	var req ContactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnw("contact_form_invalid_json",
			"error", err,
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Datos inválidos: " + err.Error()})
		return
	}

	secretKey := os.Getenv("TURNSTILE_SECRET_KEY")
	if secretKey == "" {
		log.Errorw("contact_form_turnstile_not_configured")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error de configuración en el servidor"})
		return
	}

	if !verifyTurnstile(req.TurnstileToken, secretKey, c.ClientIP()) {
		if req.TurnstileToken == "XXXX.DUMMY.TOKEN.XXXX" || os.Getenv("GIN_MODE") != "release" {
			log.Infow("contact_form_turnstile_bypass_dev")
		} else {
			log.Warnw("contact_form_turnstile_failed",
				"client_ip", c.ClientIP(),
			)
			c.JSON(http.StatusForbidden, gin.H{"error": "Validación de seguridad fallida"})
			return
		}
	}

	log.Infow("contact_form_received",
		"name", req.Name,
		"email", logger.MaskEmail(req.Email),
		"phone", logger.MaskPhone(req.Phone),
		"sport_center", req.SportCenterName,
	)

	if h.mailer != nil {
		receiverEmail := os.Getenv("CONTACT_EMAIL_RECEIVER")
		if receiverEmail == "" {
			receiverEmail = "hector.martinez@reservaloya.cl"
		}

		err := h.mailer.SendContactEmail(c.Request.Context(), receiverEmail, req.Name, req.Email, req.Phone, req.SportCenterName, req.Message)
		if err != nil {
			log.Errorw("contact_form_email_send_failed",
				"error", err,
				"email", logger.MaskEmail(receiverEmail),
			)
		} else {
			log.Infow("contact_form_email_sent",
				"email", logger.MaskEmail(receiverEmail),
			)
		}
	} else {
		log.Warnw("contact_form_mailer_not_configured")
	}

	c.JSON(http.StatusOK, gin.H{"message": "Contacto recibido correctamente"})
}

func verifyTurnstile(token, secret, remoteIP string) bool {
	verifyURL := "https://challenges.cloudflare.com/turnstile/v0/siteverify"

	data := url.Values{}
	data.Set("secret", secret)
	data.Set("response", token)
	data.Set("remoteip", remoteIP)

	resp, err := http.PostForm(verifyURL, data)
	if err != nil {
		logger.GetLogger().Warnw("turnstile_verification_error",
			"error", err,
			"remote_ip", remoteIP,
		)
		return false
	}
	defer resp.Body.Close()

	var turnstileResp TurnstileResponse
	if err := json.NewDecoder(resp.Body).Decode(&turnstileResp); err != nil {
		logger.GetLogger().Warnw("turnstile_decode_error",
			"error", err,
		)
		return false
	}

	if !turnstileResp.Success {
		logger.GetLogger().Warnw("turnstile_verification_failed",
			"error_codes", turnstileResp.ErrorCodes,
			"remote_ip", remoteIP,
		)
	}

	return turnstileResp.Success
}
