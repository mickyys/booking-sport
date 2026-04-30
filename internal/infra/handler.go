package infra

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
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
	useCase *app.SportCenterUseCase
	baseHandler *BaseHandler
}

func NewSportCenterHandler(uc *app.SportCenterUseCase) *SportCenterHandler {
	return &SportCenterHandler{
		useCase: uc,
		baseHandler: NewBaseHandler(),
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

	response, err := h.useCase.ListSportCentersPaged(c.Request.Context(), page, limit, name, city, date, hour)
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

// UpdateSportCenterSettings actualiza la configuración de un centro deportivo
func (h *SportCenterHandler) UpdateSportCenterSettings(c *gin.Context) {
	idStr := c.Param("id")
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	var body struct {
		Slug                  *string `json:"slug"`
		CancellationHours     *int    `json:"cancellation_hours"`
		RetentionPercent      *int    `json:"retention_percent"`
		PartialPaymentEnabled *bool   `json:"partial_payment_enabled"`
		PartialPaymentPercent *int    `json:"partial_payment_percent"`
		ImageURL              *string `json:"image_url"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.useCase.UpdateSettings(c.Request.Context(), id, body.Slug, body.CancellationHours, body.RetentionPercent, body.PartialPaymentEnabled, body.PartialPaymentPercent, body.ImageURL); err != nil {
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
	useCase *app.CourtUseCase
	baseHandler *BaseHandler
}

func NewCourtHandler(uc *app.CourtUseCase) *CourtHandler {
	return &CourtHandler{
		useCase: uc,
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

type UserHandler struct {
	useCase *app.UserUseCase
}

func NewUserHandler(uc *app.UserUseCase) *UserHandler {
	return &UserHandler{useCase: uc}
}
