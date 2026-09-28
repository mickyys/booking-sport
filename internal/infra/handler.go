package infra

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/internal/infra/auth0mgmt"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Auth0User struct {
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Picture   string `json:"picture"`
	LastLogin string `json:"last_login,omitempty"`
}

type SportCenterHandler struct {
	useCase     *app.SportCenterUseCase
	baseHandler *BaseHandler
	auth0       auth0mgmt.Client
	mailer      app.Mailer
}

func NewSportCenterHandler(uc *app.SportCenterUseCase, auth0 auth0mgmt.Client, mailer app.Mailer) *SportCenterHandler {
	return &SportCenterHandler{
		useCase:     uc,
		baseHandler: NewBaseHandler(),
		auth0:       auth0,
		mailer:      mailer,
	}
}

func (h *SportCenterHandler) List(c *gin.Context) {
	pageStr := c.Query("page")
	limitStr := c.Query("limit")
	name := c.Query("name")
	city := c.Query("city")
	dateStr := c.Query("date")
	hourStr := c.Query("hour")

	page, _ := strconv.Atoi(pageStr)
	limit, _ := strconv.Atoi(limitStr)

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}

	var date *time.Time
	if dateStr != "" {
		loc, _ := time.LoadLocation("America/Santiago")
		parsedDate, err := time.ParseInLocation("2006-01-02", dateStr, loc)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, expected YYYY-MM-DD"})
			return
		}
		// Normalize to Santiago midnight
		santiagoDate := time.Date(parsedDate.Year(), parsedDate.Month(), parsedDate.Day(), 0, 0, 0, 0, loc)
		date = &santiagoDate
	}

	var hour *int
	if hourStr != "" {
		hInt, err := strconv.Atoi(hourStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hour format, expected integer hour (e.g. 6)"})
			return
		}
		if hInt < 0 || hInt > 23 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "hour must be between 0 and 23"})
			return
		}
		hour = &hInt
	}

	// If hour is provided but date is not, default to today in America/Santiago
	if hour != nil && date == nil {
		loc, _ := time.LoadLocation("America/Santiago")
		now := time.Now().In(loc)
		// We set the date to Santiago midnight for consistency with MongoDB storage
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		date = &today
	}

	var lat, lng *float64
	latStr := c.Query("lat")
	lngStr := c.Query("lng")
	if latStr != "" || lngStr != "" {
		if latStr == "" || lngStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "lat and lng must be provided together"})
			return
		}
		latF, err := strconv.ParseFloat(latStr, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid lat format"})
			return
		}
		lngF, err := strconv.ParseFloat(lngStr, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid lng format"})
			return
		}
		if latF < -90 || latF > 90 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "lat must be between -90 and 90"})
			return
		}
		if lngF < -180 || lngF > 180 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "lng must be between -180 and 180"})
			return
		}
		lat, lng = &latF, &lngF
	}

	response, err := h.useCase.ListSportCentersPaged(c.Request.Context(), page, limit, name, city, date, hour, lat, lng)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *SportCenterHandler) ListCities(c *gin.Context) {
	cities, err := h.useCase.ListCities(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"cities": cities})
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
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	center := body.SportCenter

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

// validarClave verifica que la clave definida por el usuario cumpla una
// política mínima compatible con Auth0: al menos 8 caracteres con mayúscula,
// minúscula y número.
func validarClave(clave string) bool {
	var hasUpper, hasLower, hasDigit bool
	for _, r := range clave {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsNumber(r):
			hasDigit = true
		}
	}
	return len(clave) >= 8 && hasUpper && hasLower && hasDigit
}

// tenantSiteURL construye la URL pública del sitio de un centro a partir de su
// slug y PLATFORM_DOMAIN (default reservaloya.cl), misma convención de
// tenant_usecase.go.
func tenantSiteURL(slug string) string {
	if slug == "" {
		return ""
	}
	domain := strings.TrimSpace(os.Getenv("PLATFORM_DOMAIN"))
	if domain == "" {
		domain = "reservaloya.cl"
	}
	return "https://" + slug + "." + domain
}

func (h *SportCenterHandler) OnboardCreate(c *gin.Context) {
	var body struct {
		Nombre    string `json:"nombre" binding:"required,min=1,max=100"`
		Rut       string `json:"rut" binding:"required"`
		Direccion string `json:"direccion" binding:"required,min=1"`
		Email    string   `json:"email" binding:"required,email"`
		Slug     string   `json:"slug,omitempty"`
		Ciudad   string   `json:"ciudad" binding:"max=100"`
		Latitud  *float64 `json:"latitud"`
		Longitud *float64 `json:"longitud"`
		Clave    string   `json:"clave,omitempty"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	adminEmail := strings.ToLower(strings.TrimSpace(body.Email))

	// Ciudad y coordenadas las determina el frontend al seleccionar una
	// sugerencia de Mapbox. Si vienen, deben venir completas y en rango.
	if (body.Latitud == nil) != (body.Longitud == nil) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "debes enviar latitud y longitud juntas"})
		return
	}
	if body.Latitud != nil && (*body.Latitud < -90 || *body.Latitud > 90) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "latitud debe estar entre -90 y 90"})
		return
	}
	if body.Longitud != nil && (*body.Longitud < -180 || *body.Longitud > 180) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "longitud debe estar entre -180 y 180"})
		return
	}

	auth0User, err := h.auth0.FindByEmail(c.Request.Context(), adminEmail)
	if err != nil {
		log.Printf("[ONBOARD] error consultando Auth0 para %s: %v", adminEmail, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "no se pudo validar tu cuenta en este momento, intenta nuevamente"})
		return
	}
	isNewAccount := auth0User == nil

	if auth0User == nil {
		// Cuenta nueva: la clave es obligatoria y la define el usuario en el
		// formulario. Se crea la cuenta directamente al completar el registro.
		if !validarClave(body.Clave) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "debes definir una clave de al menos 8 caracteres con mayúscula, minúscula y número",
			})
			return
		}
		created, err := h.auth0.CreateAccount(c.Request.Context(), adminEmail, body.Clave)
		if err != nil {
			log.Printf("[ONBOARD] error creando cuenta Auth0 para %s: %v", adminEmail, err)
			c.JSON(http.StatusBadGateway, gin.H{"error": "no se pudo crear tu cuenta en este momento, intenta nuevamente"})
			return
		}
		auth0User = created
	} else if existingCenter, centerErr := h.useCase.FindByAdminEmail(c.Request.Context(), adminEmail); centerErr == nil && existingCenter != nil {
		// El correo ya administra un centro: no permitir crear otro.
		c.JSON(http.StatusConflict, gin.H{"error": "este correo ya está registrado en un centro deportivo"})
		return
	}

	// Rol de administrador (idempotente). Si no hay rol configurado,
	// AssignRole hace log WARN y no bloquea el flujo.
	if err := h.auth0.AssignRole(c.Request.Context(), auth0User.ID, os.Getenv("AUTH0_ADMIN_ROLE_ID")); err != nil {
		log.Printf("[ONBOARD] error asignando rol Auth0 a %s: %v", auth0User.ID, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "no se pudo asignar el rol de administrador, intenta nuevamente"})
		return
	}

	var lat, lng float64
	if body.Latitud != nil && body.Longitud != nil {
		lat, lng = *body.Latitud, *body.Longitud
	}

	center, err := h.useCase.CreateSportCenterOnboard(c.Request.Context(),
		body.Nombre,
		body.Rut,
		body.Direccion,
		body.Slug,
		adminEmail,
		auth0User.ID,
		strings.TrimSpace(body.Ciudad),
		lat,
		lng,
	)
	if err != nil {
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "RUT inválido"):
			c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
		case strings.Contains(errMsg, "correo inválido"):
			c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
		case strings.Contains(errMsg, "correo ya registrado"):
			c.JSON(http.StatusConflict, gin.H{"error": "este correo ya está registrado en un centro deportivo"})
		case strings.Contains(errMsg, "ya está en uso") && strings.Contains(errMsg, "slug"):
			c.JSON(http.StatusConflict, gin.H{"error": errMsg})
		case strings.Contains(errMsg, "ya existe un centro"):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": errMsg})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
		}
		return
	}

	// Correo de bienvenida vía Mailgun: uno solo por creación de centro,
	// best-effort (si falla, el registro igualmente se confirma y el error
	// queda en los logs). Para cuentas nuevas incluye el enlace de Auth0
	// para configurar la clave (ticket sin correo de Auth0).
	loginURL := strings.TrimRight(os.Getenv("URL_FRONTEND"), "/") + "/admin"
	welcome := app.CenterWelcomeEmail{
		To:         adminEmail,
		UserName:   auth0User.Name,
		CenterName: center.Name,
		Address:    center.Address,
		TenantURL:  tenantSiteURL(center.Slug),
		LoginURL:   loginURL,
	}
	if isNewAccount {
		setupURL, ticketErr := h.auth0.CreatePasswordSetupTicket(c.Request.Context(), auth0User.ID)
		if ticketErr != nil {
			log.Printf("[ONBOARD] centro creado pero falló el ticket de configuración de clave para %s: %v", adminEmail, ticketErr)
		} else {
			welcome.SetupURL = setupURL
		}
	}
	var welcomeErr error
	if h.mailer != nil {
		welcomeErr = h.mailer.SendCenterOnboardWelcome(c.Request.Context(), welcome)
	}
	if welcomeErr != nil {
		log.Printf("[ONBOARD] centro creado pero falló el correo de bienvenida para %s: %v", adminEmail, welcomeErr)
	}

	// Return response matching the spec
	coord := gin.H{
		"lat": center.Coordinates.Lat,
		"lng": center.Coordinates.Lng,
	}
	if center.Coordinates.Lat == 0 && center.Coordinates.Lng == 0 {
		coord = nil
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":          center.ID.Hex(),
		"slug":        center.Slug,
		"name":        center.Name,
		"city":        center.City,
		"coordinates": coord,
		"message":     "Centro creado exitosamente",
	})
}

// OnboardVerificationStatus consulta si ya existe una cuenta para el correo
// en Auth0, si fue verificada y si ya administra un centro deportivo. Lo usa
// el frontend del onboarding para bloquear correos ya registrados.
func (h *SportCenterHandler) OnboardVerificationStatus(c *gin.Context) {
	emailParam := c.Query("email")
	email := strings.ToLower(strings.TrimSpace(emailParam))
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email es requerido"})
		return
	}

	user, err := h.auth0.FindByEmail(c.Request.Context(), email)
	if err != nil {
		log.Printf("[ONBOARD] error consultando estado de cuenta para %s: %v", email, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "no se pudo consultar el estado de la cuenta en este momento, intenta nuevamente"})
		return
	}

	hasCenter := false
	if user != nil {
		center, centerErr := h.useCase.FindByAdminEmail(c.Request.Context(), email)
		if centerErr == nil && center != nil {
			hasCenter = true
		}
	}

	exists := user != nil
	verified := exists && user.EmailVerified
	c.JSON(http.StatusOK, gin.H{
		"exists":     exists,
		"verified":   verified,
		"has_center": hasCenter,
	})
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
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	center := body.SportCenter

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

// UpdateSportCenterSettings actualiza la configuración de un centro deportivo
func (h *SportCenterHandler) UpdateSportCenterSettings(c *gin.Context) {
	idStr := c.Param("id")
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	var body struct {
		Slug                  *string                     `json:"slug"`
		CancellationHours     *int                        `json:"cancellation_hours"`
		RetentionPercent      *int                        `json:"retention_percent"`
		PartialPaymentEnabled *bool                       `json:"partial_payment_enabled"`
		PartialPaymentPercent *int                        `json:"partial_payment_percent"`
		ImageURL              *string                     `json:"image_url"`
		MarketplaceFeePercent *int                        `json:"marketplace_fee_percent"`
		IsFeatured            *bool                       `json:"is_featured"`
		Branding              *domain.SportCenterBranding `json:"branding"`
		Address               *string                     `json:"address"`
		City                  *string                     `json:"city"`
		Coordinates           *domain.Coordinates         `json:"coordinates"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.useCase.UpdateSettings(c.Request.Context(), id, body.Slug, body.CancellationHours, body.RetentionPercent, body.PartialPaymentEnabled, body.PartialPaymentPercent, body.ImageURL, body.MarketplaceFeePercent, body.IsFeatured, body.Branding, body.Address, body.City, body.Coordinates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Settings updated successfully"})
}

func (h *SportCenterHandler) GetMySportCenter(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in token"})
		return
	}
	userIDStr := userID.(string)

	centers, err := h.useCase.FindByUserID(c.Request.Context(), userIDStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if len(centers) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "No sport center associated with this user"})
		return
	}

	// Por ahora retornamos el primero asociado
	c.JSON(http.StatusOK, gin.H{"center": centers[0]})
}

// UpdateMySportCenterOnboarding persiste una etapa del wizard post-login
// (RES-23..27) para el centro del usuario autenticado. Cada "Guardar y
// continuar" envía step + los datos de esa etapa; finalize marca el centro
// como completado.
func (h *SportCenterHandler) UpdateMySportCenterOnboarding(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in token"})
		return
	}
	userIDStr := userID.(string)

	var body struct {
		Step           int                      `json:"step"`
		Nombre         *string                  `json:"nombre,omitempty"`
		Rut            *string                  `json:"rut,omitempty"`
		Email          *string                  `json:"email,omitempty"`
		Direccion      *string                  `json:"direccion,omitempty"`
		Telefono       *string                  `json:"telefono,omitempty"`
		Templates     []domain.SpaceTemplate `json:"templates,omitempty"`
		PaymentMode    *string                  `json:"payment_mode,omitempty"`
		ImageURL       *string                  `json:"image_url,omitempty"`
		FirstMonthFree *bool                    `json:"first_month_free,omitempty"`
		Finalize       bool                     `json:"finalize,omitempty"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	input := app.UpdateOnboardingInput{
		Step:           body.Step,
		Nombre:         body.Nombre,
		Rut:            body.Rut,
		Email:          body.Email,
		Direccion:      body.Direccion,
		Telefono:       body.Telefono,
		Templates:     body.Templates,
		PaymentMode:    body.PaymentMode,
		ImageURL:       body.ImageURL,
		FirstMonthFree: body.FirstMonthFree,
		Finalize:       body.Finalize,
	}

	if err := h.useCase.UpdateSportCenterOnboarding(c.Request.Context(), userIDStr, input); err != nil {
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "no se encontró un centro"):
			c.JSON(http.StatusNotFound, gin.H{"error": errMsg})
		case strings.Contains(errMsg, "inválid"), strings.Contains(errMsg, "te falta"), strings.Contains(errMsg, "registrar al menos"), strings.Contains(errMsg, "cantidad de"), strings.Contains(errMsg, "horarios de"), strings.Contains(errMsg, "valores de"), strings.Contains(errMsg, "duplicado"):
			c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
		default:
			log.Printf("[ONBOARD] error actualizando onboarding para %s: %v", userIDStr, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo guardar tu progreso, intenta nuevamente"})
		}
		return
	}

	message := "onboarding actualizado"
	if body.Finalize {
		message = "onboarding completado"
	}
	c.JSON(http.StatusOK, gin.H{"message": message})
}

func (h *SportCenterHandler) GetByID(c *gin.Context) {
	idStr := c.Param("id")
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	center, err := h.useCase.FindByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Sport center not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"center": center})
}

func (h *SportCenterHandler) GetCenterUsers(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in token"})
		return
	}
	userIDStr := userID.(string)

	centers, err := h.useCase.FindByUserID(c.Request.Context(), userIDStr)
	if err != nil || len(centers) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "No sport center associated with this user"})
		return
	}

	center := centers[0]
	log.Printf("GetCenterUsers: userID=%s, center=%s, users=%v", userIDStr, center.Name, center.Users)

	auth0Domain := os.Getenv("AUTH0_DOMAIN")

	users := make([]Auth0User, 0, len(center.Users))

	for _, uID := range center.Users {
		auth0User := Auth0User{UserID: uID}

		userData := fetchAuth0UserByID(auth0Domain, uID)
		log.Printf("GetCenterUsers: other user %s - name=%s, picture=%s, last_login=%s", uID, userData.Name, userData.Picture, userData.LastLogin)
		if userData != nil {
			auth0User.Name = userData.Name
			auth0User.Email = userData.Email
			auth0User.Picture = userData.Picture
			auth0User.LastLogin = userData.LastLogin
		}

		users = append(users, auth0User)
	}

	c.JSON(http.StatusOK, gin.H{"users": users})
}

func getString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func getAuth0UserName(userID string) string {
	return userID
}

func (h *SportCenterHandler) RemoveCenterUser(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in token"})
		return
	}
	userIDStr := userID.(string)

	userIDToRemove := c.Param("userId")
	if userIDToRemove == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID is required"})
		return
	}

	centers, err := h.useCase.FindByUserID(c.Request.Context(), userIDStr)
	if err != nil || len(centers) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "No sport center associated with this user"})
		return
	}

	center := centers[0]

	found := false
	newUsers := make([]string, 0, len(center.Users))
	for _, u := range center.Users {
		if u != userIDToRemove {
			newUsers = append(newUsers, u)
		} else {
			found = true
		}
	}

	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found in sport center"})
		return
	}

	center.Users = newUsers
	if err := h.useCase.UpdateSportCenter(c.Request.Context(), center.ID, &center); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove user: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User removed successfully"})
}

func fetchAuth0Users(userIDs []string) ([]Auth0User, error) {
	auth0Domain := os.Getenv("AUTH0_DOMAIN")
	auth0Audience := os.Getenv("AUTH0_AUDIENCE")
	auth0ClientID := os.Getenv("AUTH0_CLIENT_ID_MACHINE")
	auth0ClientSecret := os.Getenv("AUTH0_CLIENT_SECRET_MACHINE")

	log.Printf("fetchAuth0Users: domain=%s, clientID=%s", auth0Domain, auth0ClientID)

	if auth0Domain == "" || auth0ClientID == "" || auth0ClientSecret == "" {
		return nil, fmt.Errorf("AUTH0 machine credentials not configured")
	}

	tokenURL := "https://" + auth0Domain + "/oauth/token"
	managementAudience := auth0Audience
	tokenReq := map[string]string{
		"grant_type":    "client_credentials",
		"client_id":     auth0ClientID,
		"client_secret": auth0ClientSecret,
		"audience":      managementAudience,
	}

	tokenReqBody, _ := json.Marshal(tokenReq)
	tokenResp, err := http.Post(tokenURL, "application/json", bytes.NewBuffer(tokenReqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %v", err)
	}
	defer tokenResp.Body.Close()

	var tokenData struct {
		AccessToken string `json:"access_token"`
	}

	decoder := json.NewDecoder(tokenResp.Body)
	if err := decoder.Decode(&tokenData); err != nil {
		log.Printf("Auth0 token decode error: %v", err)
		return nil, fmt.Errorf("failed to decode token response: %v", err)
	}

	log.Printf("Auth0 M2M token response status: %d", tokenResp.StatusCode)
	if tokenData.AccessToken == "" {
		log.Printf("Auth0 M2M token empty - app needs client_credentials grant and read:users permission")
		return nil, fmt.Errorf("empty access token")
	}

	client := &http.Client{}
	users := make([]Auth0User, 0, len(userIDs))

	for _, userID := range userIDs {
		userURL := "https://" + auth0Domain + "/api/v2/users/" + userID
		req, _ := http.NewRequest("GET", userURL, nil)
		req.Header.Set("Authorization", "Bearer "+tokenData.AccessToken)

		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			continue
		}

		var userData struct {
			UserID  string `json:"user_id"`
			Name    string `json:"name"`
			Email   string `json:"email"`
			Picture string `json:"picture"`
		}
		userDecoder := json.NewDecoder(resp.Body)
		if err := userDecoder.Decode(&userData); err == nil {
			users = append(users, Auth0User{
				UserID:  userData.UserID,
				Name:    userData.Name,
				Email:   userData.Email,
				Picture: userData.Picture,
			})
		}
	}

	_ = tokenReqBody
	return users, nil
}

func fetchAuth0UserByID(domain, userID string) *Auth0User {
	if domain == "" || userID == "" {
		return nil
	}

	auth0ClientID := os.Getenv("AUTH0_CLIENT_ID_MACHINE")
	auth0ClientSecret := os.Getenv("AUTH0_CLIENT_SECRET_MACHINE")
	auth0Audience := os.Getenv("AUTH0_AUDIENCE")
	managementAudience := auth0Audience

	log.Printf("fetchAuth0UserByID: using machine credentials, clientID=%s, audience=%s", auth0ClientID, managementAudience)

	if auth0ClientID == "" || auth0ClientSecret == "" {
		log.Printf("fetchAuth0UserByID: machine credentials not configured")
		return nil
	}

	tokenURL := "https://" + domain + "/oauth/token"
	tokenReq := map[string]string{
		"grant_type":    "client_credentials",
		"client_id":     auth0ClientID,
		"client_secret": auth0ClientSecret,
		"audience":      managementAudience,
	}

	tokenReqBody, _ := json.Marshal(tokenReq)
	tokenResp, err := http.Post(tokenURL, "application/json", bytes.NewBuffer(tokenReqBody))
	if err != nil {
		log.Printf("fetchAuth0UserByID: failed to get token: %v", err)
		return nil
	}
	defer tokenResp.Body.Close()

	var tokenData struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenData); err != nil {
		log.Printf("fetchAuth0UserByID: failed to decode token: %v", err)
		return nil
	}

	if tokenData.AccessToken == "" {
		log.Printf("fetchAuth0UserByID: empty token for user %s", userID)
		return nil
	}

	log.Printf("fetchAuth0UserByID: fetching user %s from %s", userID, domain)
	userURL := "https://" + domain + "/api/v2/users/" + userID
	req, _ := http.NewRequest("GET", userURL, nil)
	req.Header.Set("Authorization", "Bearer "+tokenData.AccessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("fetchAuth0UserByID: request error for %s: %v", userID, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("fetchAuth0UserByID: response status %d for user %s, body: %s", resp.StatusCode, userID, string(body))
		return nil
	}

	var userData struct {
		UserID    string `json:"user_id"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		Picture   string `json:"picture"`
		LastLogin string `json:"last_login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&userData); err != nil {
		log.Printf("fetchAuth0UserByID: decode error for %s: %v", userID, err)
		return nil
	}

	log.Printf("fetchAuth0UserByID: got user %s, name=%s, email=%s, last_login=%s", userID, userData.Name, userData.Email, userData.LastLogin)

	return &Auth0User{
		UserID:    userData.UserID,
		Name:      userData.Name,
		Email:     userData.Email,
		Picture:   userData.Picture,
		LastLogin: userData.LastLogin,
	}
}

type CourtHandler struct {
	useCase     *app.CourtUseCase
	baseHandler *BaseHandler
}

func NewCourtHandler(uc *app.CourtUseCase) *CourtHandler {
	return &CourtHandler{
		useCase:     uc,
		baseHandler: NewBaseHandler(),
	}
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

func (h *CourtHandler) UpdateScheduleSlot(c *gin.Context) {
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

	var slot domain.CourtSchedule
	if err := c.ShouldBindJSON(&slot); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validar horario
	if slot.Hour < 0 || slot.Hour > 23 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Hour must be between 0 and 23"})
		return
	}
	if slot.Minutes < 0 || slot.Minutes > 59 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Minutes must be between 0 and 59"})
		return
	}

	if err := h.useCase.UpdateScheduleSlot(c.Request.Context(), courtID, slot, userIDStr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Schedule slot updated successfully"})
}

func (h *CourtHandler) GetSchedule(c *gin.Context) {
	idStr := c.Param("id")
	courtID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid court ID format"})
		return
	}

	loc, _ := time.LoadLocation("America/Santiago")
	dateStr := c.Query("date")
	date := time.Now().In(loc)
	if dateStr != "" {
		parsedDate, err := time.ParseInLocation("2006-01-02", dateStr, loc)
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
		ImageURL      string             `json:"image_url"`
		YPosition     int                `json:"y_position"`
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	court := &domain.Court{
		SportCenterID: body.SportCenterID,
		Name:          body.Name,
		Description:   body.Description,
		ImageURL:      body.ImageURL,
		YPosition:     body.YPosition,
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
		ImageURL    string `json:"image_url"`
		YPosition   int    `json:"y_position"`
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updatedCourt := &domain.Court{
		Name:        body.Name,
		Description: body.Description,
		ImageURL:    body.ImageURL,
		YPosition:   body.YPosition,
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
	var centerID primitive.ObjectID
	// Try parse as ObjectID first; if fails, treat as slug and resolve
	oid, err := primitive.ObjectIDFromHex(idStr)
	if err == nil {
		centerID = oid
	} else {
		// treat idStr as slug
		center, findErr := h.useCase.FindBySlug(c.Request.Context(), idStr)
		if findErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid sport center identifier"})
			return
		}
		centerID = center.ID
	}

	loc, _ := time.LoadLocation("America/Santiago")
	dateStr := c.Query("date")
	date := time.Now().In(loc)
	if dateStr != "" {
		parsedDate, err := time.ParseInLocation("2006-01-02", dateStr, loc)
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

// GetSchedulesWithBookings retorna schedules con detalles del cliente
// Requiere autenticación y el usuario debe estar asociado al centro deportivo
func (h *SportCenterHandler) GetSchedulesWithBookings(c *gin.Context) {
	idStr := c.Param("id")
	var centerID primitive.ObjectID
	// Try parse as ObjectID first; if fails, treat as slug and resolve
	oid, err := primitive.ObjectIDFromHex(idStr)
	if err == nil {
		centerID = oid
	} else {
		center, findErr := h.useCase.FindBySlug(c.Request.Context(), idStr)
		if findErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid sport center identifier"})
			return
		}
		centerID = center.ID
	}

	// Authorization: user must be associated to the center
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

	// Verify user is owner of the center
	center, err := h.useCase.FindByID(c.Request.Context(), centerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Sport center not found"})
		return
	}
	authorized := false
	for _, u := range center.Users {
		if u == userIDStr {
			authorized = true
			break
		}
	}
	if !authorized {
		c.JSON(http.StatusForbidden, gin.H{"error": "user not authorized for this sport center"})
		return
	}

	dateStr := c.Query("date")
	loc, _ := time.LoadLocation("America/Santiago")
	date := time.Now().In(loc)
	if dateStr != "" {
		parsedDate, err := time.ParseInLocation("2006-01-02", dateStr, loc)
		if err == nil {
			date = parsedDate
		}
	}

	all := c.Query("all") == "true"
	schedules, err := h.useCase.GetSportCenterSchedulesWithBookingDetails(c.Request.Context(), centerID, date, all)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, schedules)
}

// GetAdminSchedulesWithBookings retorna schedules con detalles del cliente
// para los centros asociados al usuario autenticado. Si se pasa centerId, solo retorna ese centro.
func (h *SportCenterHandler) GetAdminSchedulesWithBookings(c *gin.Context) {
	// Obtener user_id del contexto
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

	// Obtener centros asociados al usuario
	centers, err := h.useCase.FindByUserID(c.Request.Context(), userIDStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(centers) == 0 {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}

	// Parámetros de fecha, 'all' y 'centerId' opcional
	centerIDParam := c.Query("centerId")
	dateStr := c.Query("date")
	loc, _ := time.LoadLocation("America/Santiago")
	date := time.Now().In(loc)
	if dateStr != "" {
		parsedDate, err := time.ParseInLocation("2006-01-02", dateStr, loc)
		if err == nil {
			date = parsedDate
		}
	}

	all := c.Query("all") == "true"

	// Si se pasó centerId, filtrar solo ese centro
	var filteredCenters []domain.SportCenter
	if centerIDParam != "" {
		centerID, err := primitive.ObjectIDFromHex(centerIDParam)
		if err == nil {
			for _, c := range centers {
				if c.ID == centerID {
					filteredCenters = append(filteredCenters, c)
					break
				}
			}
		}
		if len(filteredCenters) == 0 {
			c.JSON(http.StatusForbidden, gin.H{"error": "Centro no autorizado"})
			return
		}
	} else {
		filteredCenters = centers
	}

	// Soporte por rango de fechas: si se pasan `from` y `to`, se retorna un
	// slice de { date, courts } para cada fecha del rango en una sola llamada.
	fromStr := c.Query("from")
	toStr := c.Query("to")
	if fromStr != "" && toStr != "" {
		loc, _ := time.LoadLocation("America/Santiago")
		from, errFrom := time.ParseInLocation("2006-01-02", fromStr, loc)
		to, errTo := time.ParseInLocation("2006-01-02", toStr, loc)
		if errFrom != nil || errTo != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid from/to date format, expected YYYY-MM-DD"})
			return
		}

		byDate := make(map[string][]app.CourtScheduleResponse)
		for _, center := range filteredCenters {
			rangeResults, err := h.useCase.GetSportCenterSchedulesWithBookingDetailsRange(c.Request.Context(), center.ID, from, to, all)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			for _, rr := range rangeResults {
				byDate[rr.Date] = append(byDate[rr.Date], rr.Courts...)
			}
		}

		dates := make([]string, 0, len(byDate))
		for d := range byDate {
			dates = append(dates, d)
		}
		sort.Strings(dates)

		result := make([]app.CourtScheduleByDate, 0, len(dates))
		for _, d := range dates {
			result = append(result, app.CourtScheduleByDate{Date: d, Courts: byDate[d]})
		}
		c.JSON(http.StatusOK, result)
		return
	}

	// Collect all schedules from filtered centers (same format as public endpoint)
	var allSchedules []app.CourtScheduleResponse
	for _, center := range filteredCenters {
		schedules, err := h.useCase.GetSportCenterSchedulesWithBookingDetails(c.Request.Context(), center.ID, date, all)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		allSchedules = append(allSchedules, schedules...)
	}

	c.JSON(http.StatusOK, allSchedules)
}

type SpaceHandler struct {
	useCase *app.SpaceUseCase
}

func NewSpaceHandler(useCase *app.SpaceUseCase) *SpaceHandler {
	return &SpaceHandler{useCase: useCase}
}

// GetAdminSpaces lista los espacios (canchas, quinchos, sedes y piscinas) del
// centro del admin autenticado.
func (h *SpaceHandler) GetAdminSpaces(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return
	}
	spaces, err := h.useCase.ListByUser(c.Request.Context(), userID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if spaces == nil {
		spaces = []domain.Space{}
	}
	c.JSON(http.StatusOK, gin.H{"spaces": spaces})
}

// Create crea un espacio (cancha, quincho, sede o piscina) para el centro del admin.
func (h *SpaceHandler) Create(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return
	}

	var body struct {
		SportCenterID string `json:"sport_center_id"`
		domain.Space
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	centerID, err := primitive.ObjectIDFromHex(body.SportCenterID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid sport_center_id format"})
		return
	}

	if body.Category == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "category is required (cancha, quincho, sede, piscina)"})
		return
	}

	space := body.Space
	space.SportCenterID = centerID

	if err := h.useCase.Create(c.Request.Context(), &space, userID.(string)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"space": space})
}

// Update actualiza un espacio por id.
func (h *SpaceHandler) Update(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return
	}

	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid space ID format"})
		return
	}

	var body struct {
		domain.Space
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	space := body.Space

	if err := h.useCase.Update(c.Request.Context(), id, &space, userID.(string)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Space updated successfully"})
}

// Delete elimina un espacio por id.
func (h *SpaceHandler) Delete(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return
	}

	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid space ID format"})
		return
	}

	if err := h.useCase.Delete(c.Request.Context(), id, userID.(string)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Space deleted successfully"})
}

type UserHandler struct {
	useCase *app.UserUseCase
}

func NewUserHandler(uc *app.UserUseCase) *UserHandler {
	return &UserHandler{useCase: uc}
}
