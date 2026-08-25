// Package auth0mgmt encapsula el acceso a la Auth0 Management API v2 para el
// flujo de onboarding: búsqueda de usuarios por email, creación directa de la
// cuenta del administrador (con la clave definida en el formulario) y
// asignación del rol de administrador.
//
// La interfaz Client permite mockear en tests; ManagementClient es la
// implementación real que se autentica vía client_credentials (M2M) para la
// Management API.
package auth0mgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// DefaultConnection es la conexión de base de datos Auth0 donde se crean las
// cuentas nuevas (AUTH0_CONNECTION permite sobreescribirla).
const DefaultConnection = "Username-Password-Authentication"

// User es el subconjunto de un usuario Auth0 relevante para el onboarding.
type User struct {
	ID            string `json:"user_id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

// Client es la interfaz inyectable sobre la Auth0 Management API.
type Client interface {
	// FindByEmail busca un usuario por email. not-found → (nil, nil).
	FindByEmail(ctx context.Context, email string) (*User, error)
	// CreateAccount crea la cuenta del usuario en la conexión de base de
	// datos Auth0 con la contraseña entregada (definida por el usuario en
	// el formulario de onboarding). No dispara ningún correo de Auth0.
	// Retorna el usuario creado; si ya existía, retorna el existente.
	CreateAccount(ctx context.Context, email, password string) (*User, error)
	// CreatePasswordSetupTicket genera un ticket de cambio de contraseña
	// para que el usuario configure su acceso. No envía correo (send_email
	// false): la URL se incrusta en el correo de bienvenida de Mailgun.
	CreatePasswordSetupTicket(ctx context.Context, auth0UserID string) (string, error)
	// AssignRole asigna roleID al usuario. Idempotente: si ya lo tiene, no-op.
	AssignRole(ctx context.Context, auth0UserID, roleID string) error
}

type ManagementClient struct {
	domain       string
	clientID     string
	clientSecret string
	connection   string

	http *http.Client

	// baseURL permite apuntar a otro servidor en tests; vacío usa https://<domain>.
	baseURL string

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

var _ Client = (*ManagementClient)(nil)

// NewManagementClient construye un cliente desde variables de entorno:
// AUTH0_MGMT_DOMAIN (dominio canónico del tenant; la Management API no está
// disponible vía dominio custom; cae a AUTH0_DOMAIN si no existe),
// AUTH0_CLIENT_ID_MACHINE, AUTH0_CLIENT_SECRET_MACHINE, AUTH0_CONNECTION
// (default DefaultConnection).
func NewManagementClient() *ManagementClient {
	connection := os.Getenv("AUTH0_CONNECTION")
	if connection == "" {
		connection = DefaultConnection
	}
	mgmtDomain := strings.TrimSpace(os.Getenv("AUTH0_MGMT_DOMAIN"))
	if mgmtDomain == "" {
		mgmtDomain = strings.TrimSpace(os.Getenv("AUTH0_DOMAIN"))
	}
	return &ManagementClient{
		domain:       mgmtDomain,
		clientID:     os.Getenv("AUTH0_CLIENT_ID_MACHINE"),
		clientSecret: os.Getenv("AUTH0_CLIENT_SECRET_MACHINE"),
		connection:   connection,
		http:         &http.Client{Timeout: 10 * time.Second},
	}
}

// root retorna la URL base: el override de tests si existe, o https://<domain>.
func (c *ManagementClient) root() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return "https://" + c.domain
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// getToken retorna un access token M2M válido, renovándolo vía
// client_credentials cuando falta menos de ~60s para que expire. El mutex
// serializa renovaciones concurrentes.
func (c *ManagementClient) getToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		return c.accessToken, nil
	}

	if c.domain == "" || c.clientID == "" || c.clientSecret == "" {
		return "", fmt.Errorf("auth0: credenciales M2M no configuradas (AUTH0_DOMAIN, AUTH0_CLIENT_ID_MACHINE, AUTH0_CLIENT_SECRET_MACHINE)")
	}

	body, _ := json.Marshal(map[string]string{
		"grant_type":    "client_credentials",
		"client_id":     c.clientID,
		"client_secret": c.clientSecret,
		"audience":      "https://" + c.domain + "/api/v2/",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.root()+"/oauth/token", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("auth0: construyendo request de token: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("auth0: solicitando token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(b))
		if resp.StatusCode == http.StatusForbidden {
			return "", fmt.Errorf("auth0: token endpoint respondió 403: %s (¿la app M2M tiene autorizada la Auth0 Management API y es AUTH0_MGMT_DOMAIN el dominio canónico del tenant?)", msg)
		}
		return "", fmt.Errorf("auth0: token endpoint respondió %d: %s", resp.StatusCode, msg)
	}

	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", fmt.Errorf("auth0: decodificando respuesta de token: %w", err)
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("auth0: respuesta de token sin access_token")
	}

	c.accessToken = tok.AccessToken
	expiresIn := time.Duration(tok.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = time.Hour
	}
	c.tokenExpiry = time.Now().Add(expiresIn)
	return c.accessToken, nil
}

// apiRequest ejecuta una llamada autenticada a la Management API v2.
// out puede ser nil si no interesa el body de respuesta.
func (c *ManagementClient) apiRequest(ctx context.Context, method, path string, payload interface{}, out interface{}) error {
	if c.domain == "" {
		return fmt.Errorf("auth0: dominio no configurado (AUTH0_DOMAIN)")
	}

	token, err := c.getToken(ctx)
	if err != nil {
		return err
	}

	var reader io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("auth0: serializando payload %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.root()+"/api/v2"+path, reader)
	if err != nil {
		return fmt.Errorf("auth0: construyendo request %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("auth0: llamada %s %s falló: %w", method, path, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		// Asignar un rol que el usuario ya tiene no es un fallo: es un no-op.
		if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusConflict {
			if isRoleAlreadyAssigned(msg) {
				log.Printf("[AUTH0MGMT] rol ya asignado (no-op): %s", msg)
				return nil
			}
		}
		return fmt.Errorf("auth0: %s %s respondió %d: %s", method, path, resp.StatusCode, msg)
	}

	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("auth0: decodificando respuesta %s %s: %w", method, path, err)
		}
	}
	return nil
}

// isRoleAlreadyAssigned detecta la respuesta de Auth0 cuando el rol ya está
// presente en el usuario ("The user already has the role specified"). Exige
// la palabra "role" para no capturar otros conflictos con "already" (p.ej.
// "The user already exists." al crear cuentas duplicadas).
func isRoleAlreadyAssigned(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "already") && strings.Contains(lower, "role")
}

func (c *ManagementClient) FindByEmail(ctx context.Context, email string) (*User, error) {
	var users []User
	path := "/users-by-email?email=" + url.QueryEscape(email)
	if err := c.apiRequest(ctx, http.MethodGet, path, nil, &users); err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}
	u := users[0]
	return &u, nil
}

// isUserAlreadyExists detecta la respuesta 409 de Auth0 al intentar crear un
// usuario que ya existe ("The user already exists.").
func isUserAlreadyExists(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "already exists")
}

// createdUser es el subconjunto de la respuesta de POST /users relevante para
// el flujo: el ID del usuario recién creado.
type createdUser struct {
	ID string `json:"user_id"`
}

// CreateAccount crea la cuenta del usuario en la conexión de base de datos
// Auth0 con email_verified=true y la contraseña entregada por el usuario
// (validada previamente contra la política del backend). No envía correos:
// el correo de bienvenida lo maneja el flujo de onboarding vía Mailgun.
// Si Auth0 responde que el usuario ya existe, localiza y retorna el existente.
func (c *ManagementClient) CreateAccount(ctx context.Context, email, password string) (*User, error) {
	payload := map[string]interface{}{
		"email":          email,
		"connection":     c.connection,
		"password":       password,
		"email_verified": true,
	}

	var created createdUser
	createErr := c.apiRequest(ctx, http.MethodPost, "/users", payload, &created)
	if createErr == nil && created.ID != "" {
		return &User{ID: created.ID, Email: email, EmailVerified: true}, nil
	}
	if createErr != nil && !isUserAlreadyExists(createErr) {
		return nil, createErr
	}
	// Carrera: el usuario se creó entre FindByEmail y ahora. Retornar el
	// existente para continuar el flujo.
	log.Printf("[AUTH0MGMT] usuario %s ya existía; se retorna el usuario existente", email)
	u, findErr := c.FindByEmail(ctx, email)
	if findErr != nil {
		return nil, findErr
	}
	if u == nil {
		if createErr != nil {
			return nil, createErr
		}
		return nil, fmt.Errorf("auth0: creación de %s sin user_id ni usuario localizable", email)
	}
	return u, nil
}

func (c *ManagementClient) AssignRole(ctx context.Context, auth0UserID, roleID string) error {
	if roleID == "" {
		log.Printf("[AUTH0MGMT] WARN: AUTH0_ADMIN_ROLE_ID no configurado; se omite asignación de rol para %s", auth0UserID)
		return nil
	}
	payload := map[string][]string{"roles": {roleID}}
	return c.apiRequest(ctx, http.MethodPost, "/users/"+url.PathEscape(auth0UserID)+"/roles", payload, nil)
}

// passwordResetResultURL retorna la URL a la que Auth0 redirige tras definir
// la contraseña: URL_FRONTEND + "/admin" (login admin). Se lee por llamada
// siguiendo la convención de otros clientes (p.ej. mailgun/mailer.go).
func passwordResetResultURL() string {
	frontendURL := strings.TrimRight(os.Getenv("URL_FRONTEND"), "/")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}
	return frontendURL + "/admin"
}

// passwordSetupTicket es el subconjunto de la respuesta de POST
// /tickets/password-change relevante para el flujo: la URL del ticket.
type passwordSetupTicket struct {
	Ticket string `json:"ticket"`
}

// CreatePasswordSetupTicket genera un ticket de cambio de contraseña con
// send_email=false (Auth0 no envía nada): la URL del ticket se incrusta en el
// correo de bienvenida de Mailgun para que el usuario configure su acceso.
// Tras definirla es redirigido a passwordResetResultURL().
func (c *ManagementClient) CreatePasswordSetupTicket(ctx context.Context, auth0UserID string) (string, error) {
	ticketPayload := map[string]interface{}{
		"user_id":    auth0UserID,
		"result_url": passwordResetResultURL(),
		"send_email": false,
	}
	var ticket passwordSetupTicket
	if err := c.apiRequest(ctx, http.MethodPost, "/tickets/password-change", ticketPayload, &ticket); err != nil {
		return "", err
	}
	if ticket.Ticket == "" {
		return "", fmt.Errorf("auth0: respuesta de ticket sin URL")
	}
	return ticket.Ticket, nil
}
