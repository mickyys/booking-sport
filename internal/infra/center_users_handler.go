package infra

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/infra/auth0mgmt"
)

// randomTemporaryPassword genera una clave aleatoria que cumple la política
// (mayúscula, minúscula, número). El usuario invitado nunca la conoce: define
// la suya con el enlace del correo de invitación.
func randomTemporaryPassword() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "Aa1" + hex.EncodeToString(buf), nil
}

// inviteTraceID deriva un identificador estable a partir del correo (no
// reversible) para seguir todos los intentos de invitar a la misma persona.
func inviteTraceID(email string) string {
	sum := sha256.Sum256([]byte(email))
	return "inv-" + hex.EncodeToString(sum[:])[:10]
}

// inviteLogger antepone a cada log de la invitación el trace-id del correo y
// el X-Trace-ID de la solicitud HTTP.
func inviteLogger(traceID, requestID string) func(format string, args ...interface{}) {
	return func(format string, args ...interface{}) {
		log.Printf("[%s] [INVITE req=%s] "+format, append([]interface{}{traceID, requestID}, args...)...)
	}
}

// InviteCenterUser invita a un administrador al centro del usuario autenticado.
//
//	POST /admin/users { "email": "...", "nombre": "..." }
//
// Crea la cuenta en Auth0 si no existe, asigna el rol de administrador, agrega
// el usuario a center.Users y envía el correo de invitación.
func (h *SportCenterHandler) InviteCenterUser(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in token"})
		return
	}

	var body struct {
		Email  string `json:"email" binding:"required,email"`
		Nombre string `json:"nombre" binding:"max=100"`
		// Resend reenvía la invitación a un usuario que ya está en el centro:
		// reasigna el rol (idempotente) y envía el correo de nuevo.
		Resend bool `json:"resend"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ingresa un correo válido"})
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	nombre := strings.TrimSpace(body.Nombre)

	traceID := inviteTraceID(email)
	c.Header("X-Invite-Trace-Id", traceID)
	logf := inviteLogger(traceID, c.Writer.Header().Get("X-Trace-ID"))
	logf("paso 1/8 solicitud recibida: invitador=%v email=%s resend=%v", userID, email, body.Resend)

	ctx := auth0mgmt.WithTrace(c.Request.Context(), traceID)

	centers, err := h.useCase.FindByUserID(ctx, userID.(string))
	if err != nil || len(centers) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "No sport center associated with this user"})
		return
	}
	center := centers[0]
	logf("paso 2/8 centro del invitador: %q (%s), administradores actuales=%d", center.Name, center.ID.Hex(), len(center.Users))

	roleID := os.Getenv("AUTH0_ADMIN_ROLE_ID")
	if roleID == "" {
		logf("ERROR AUTH0_ADMIN_ROLE_ID no configurado")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "el rol de administrador no está configurado"})
		return
	}

	auth0User, err := h.auth0.FindByEmail(ctx, email)
	if err != nil {
		logf("ERROR paso 3/8 consultando Auth0 por email %s: %v", email, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "no se pudo validar la cuenta en este momento, intenta nuevamente"})
		return
	}
	isNewAccount := auth0User == nil
	if isNewAccount {
		logf("paso 3/8 Auth0: no existe cuenta con %s -> se creará", email)
	} else {
		logf("paso 3/8 Auth0: la cuenta ya existe user_id=%s email_verified=%v -> se reutiliza", auth0User.ID, auth0User.EmailVerified)
	}

	alreadyInCenter := false
	if auth0User != nil {
		for _, existing := range center.Users {
			if existing == auth0User.ID {
				alreadyInCenter = true
				break
			}
		}
		logf("paso 4/8 validaciones locales: ya en este centro=%v", alreadyInCenter)
		if alreadyInCenter && !body.Resend {
			c.JSON(http.StatusConflict, gin.H{"error": "este usuario ya es administrador de tu centro"})
			return
		}
		if !alreadyInCenter {
			others, findErr := h.useCase.FindByUserID(ctx, auth0User.ID)
			if findErr == nil && len(others) > 0 {
				logf("RECHAZADO paso 4/8: %s ya administra otro centro (%s)", auth0User.ID, others[0].Name)
				c.JSON(http.StatusConflict, gin.H{"error": "este correo ya administra otro centro deportivo"})
				return
			}
		}
	} else {
		if body.Resend {
			c.JSON(http.StatusNotFound, gin.H{"error": "no existe una cuenta con ese correo"})
			return
		}
		password, pwErr := randomTemporaryPassword()
		if pwErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo crear la cuenta"})
			return
		}
		logf("paso 4/8 creando cuenta en Auth0 para %s", email)
		created, createErr := h.auth0.CreateAccount(ctx, email, password)
		if createErr != nil {
			logf("ERROR paso 4/8 creando cuenta Auth0 para %s: %v", email, createErr)
			c.JSON(http.StatusBadGateway, gin.H{"error": "no se pudo crear la cuenta en este momento, intenta nuevamente"})
			return
		}
		auth0User = created
		logf("paso 4/8 cuenta creada: user_id=%s", auth0User.ID)
	}

	logf("paso 5/8 asignando rol %s a %s", roleID, auth0User.ID)
	if err := h.auth0.AssignRole(ctx, auth0User.ID, roleID); err != nil {
		logf("ERROR paso 5/8 asignando rol a %s: %v", auth0User.ID, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "no se pudo asignar el rol de administrador, intenta nuevamente"})
		return
	}

	// Verifica que el rol quedó asignado. Sin el scope read:roles no se puede
	// comprobar: se registra un aviso y se continúa.
	logf("paso 5/8 AssignRole respondió OK; verificando rol")
	hasRole, verifyErr := h.auth0.UserHasRole(ctx, auth0User.ID, roleID)
	switch {
	case errors.Is(verifyErr, auth0mgmt.ErrRolesUnreadable):
		logf("WARN paso 6/8 rol %s asignado a %s pero no verificado (falta read:roles en la app M2M)", roleID, auth0User.ID)
	case verifyErr != nil:
		logf("WARN paso 6/8 no se pudo verificar el rol de %s: %v", auth0User.ID, verifyErr)
	case !hasRole:
		logf("ERROR paso 6/8 Auth0 aceptó la asignación pero %s no tiene el rol %s", auth0User.ID, roleID)
		c.JSON(http.StatusBadGateway, gin.H{"error": "no se pudo confirmar el rol de administrador, intenta nuevamente"})
		return
	default:
		logf("paso 6/8 rol %s confirmado para %s", roleID, auth0User.ID)
	}

	if alreadyInCenter {
		logf("paso 7/8 el usuario ya estaba en center.Users; no se modifica el centro")
	} else {
		logf("paso 7/8 agregando %s a center.Users", auth0User.ID)
		center.Users = append(center.Users, auth0User.ID)
		if err := h.useCase.UpdateSportCenter(ctx, center.ID, &center); err != nil {
			logf("ERROR paso 7/8 agregando usuario %s al centro %s: %v", auth0User.ID, center.Name, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo agregar al usuario al centro"})
			return
		}
	}

	displayName := nombre
	if displayName == "" {
		displayName = auth0User.Name
	}
	invite := app.CenterInvitationEmail{
		To:         email,
		UserName:   displayName,
		CenterName: center.Name,
		Address:    center.Address,
		TenantURL:  tenantSiteURL(center.Slug),
		LoginURL:   strings.TrimRight(os.Getenv("URL_FRONTEND"), "/") + "/admin",
	}
	if isNewAccount || (body.Resend && strings.HasPrefix(auth0User.ID, "auth0|")) {
		setupURL, ticketErr := h.auth0.CreatePasswordSetupTicket(ctx, auth0User.ID)
		if ticketErr != nil {
			logf("WARN paso 8/8 falló el ticket de clave para %s: %v", email, ticketErr)
		} else {
			invite.SetupURL = setupURL
			logf("paso 8/8 ticket de clave generado")
		}
	}

	emailSent := false
	if h.mailer != nil {
		if mailErr := h.mailer.SendCenterInvitation(ctx, invite); mailErr != nil {
			logf("WARN paso 8/8 usuario agregado pero falló el correo para %s: %v", email, mailErr)
		} else {
			emailSent = true
			logf("paso 8/8 correo de invitación enviado (cuenta nueva=%v)", isNewAccount)
		}
	}

	logf("OK invitación completada user_id=%s email_enviado=%v", auth0User.ID, emailSent)
	status := http.StatusCreated
	if alreadyInCenter {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{
		"message":    "Administrador agregado",
		"user_id":    auth0User.ID,
		"email":      email,
		"email_sent": emailSent,
		"trace_id":   traceID,
	})
}
