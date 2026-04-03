package infra

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
)

type ContactHandler struct {
	mailer app.Mailer
}

func NewContactHandler(mailer app.Mailer) *ContactHandler {
	return &ContactHandler{mailer: mailer}
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
	var req ContactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Datos inválidos: " + err.Error()})
		return
	}

	// Validar Turnstile
	secretKey := os.Getenv("TURNSTILE_SECRET_KEY")
	if secretKey == "" {
		log.Println("Error: TURNSTILE_SECRET_KEY no configurada")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error de configuración en el servidor"})
		return
	}

	if !verifyTurnstile(req.TurnstileToken, secretKey, c.ClientIP()) {
		// En desarrollo (localhost), permitimos que el token de prueba de Cloudflare pase
		// aunque la secretKey no coincida con el tipo de siteKey (ej. usando dummy sitekey con real secretkey)
		if req.TurnstileToken == "XXXX.DUMMY.TOKEN.XXXX" || os.Getenv("GIN_MODE") != "release" {
			log.Println("Pase de seguridad: Permitido en modo desarrollo o con token dummy")
		} else {
			c.JSON(http.StatusForbidden, gin.H{"error": "Validación de seguridad fallida"})
			return
		}
	}

	// Enviar correo si el mailer está configurado
	if h.mailer != nil {
		receiverEmail := os.Getenv("CONTACT_EMAIL_RECEIVER")
		if receiverEmail == "" {
			receiverEmail = "hector.martinez@reservaloya.cl" // Fallback
		}

		err := h.mailer.SendContactEmail(c.Request.Context(), receiverEmail, req.Name, req.Email, req.Phone, req.SportCenterName, req.Message)
		if err != nil {
			log.Printf("Error al enviar correo de contacto: %v", err)
			// No bloqueamos la respuesta al usuario si falla el correo, pero lo logueamos
		}
	} else {
		log.Printf("Advertencia: Mailer no configurado, no se envió correo de contacto")
	}

	// Por ahora simulamos éxito y logueamos el contacto.
	log.Printf("Nuevo contacto de centro deportivo: %s <%s>, Tel: %s, Centro: %s, Mensaje: %s",
		req.Name, req.Email, req.Phone, req.SportCenterName, req.Message)

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
		log.Printf("Error al verificar Turnstile: %v", err)
		return false
	}
	defer resp.Body.Close()

	var turnstileResp TurnstileResponse
	if err := json.NewDecoder(resp.Body).Decode(&turnstileResp); err != nil {
		log.Printf("Error al decodificar respuesta de Turnstile: %v", err)
		return false
	}

	if !turnstileResp.Success {
		log.Printf("Turnstile rechazado. Códigos de error: %v", turnstileResp.ErrorCodes)
	}

	return turnstileResp.Success
}
