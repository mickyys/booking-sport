package mercadopago

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	mp "github.com/mercadopago/sdk-go/pkg/config"
	"github.com/mercadopago/sdk-go/pkg/user"
)

const (
	oauthTokenURL = "https://api.mercadopago.com/oauth/token"
	defaultAuth   = "https://auth.mercadopago.com"
)

// TokenResponse representa las credenciales obtenidas al conectar una cuenta de vendedor
// vía Mercado Pago Connect (OAuth).
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	PublicKey    string `json:"public_key"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
	LiveMode     bool   `json:"live_mode"`
}

// ConnectClient maneja el flujo OAuth de Mercado Pago Connect (modo marketplace),
// permitiendo que la plataforma opere en nombre de un vendedor.
type ConnectClient struct {
	clientID    string
	secretKey   string
	redirectURI string
	authBase    string
	publicKey   string
	httpClient  *http.Client
}

// NewConnectClient crea un cliente OAuth para Mercado Pago Connect.
// - clientID: ID de cliente OAuth (NEXT_PUBLIC_MP_CLIENT_ID)
// - secretKey: secreto de la aplicación (MP_CLIENT_SECRET)
// - redirectURI: URI de retorno configurada en la aplicación (MP_REDIRECT_URI)
// - authBase: dominio de autorización OAuth según país (MP_AUTH_BASE, ej. https://auth.mercadopago.cl)
// - publicKey: public key de la plataforma (NEXT_PUBLIC_MP_PUBLIC_KEY)
func NewConnectClient(clientID, secretKey, redirectURI, authBase, publicKey string) *ConnectClient {
	if authBase == "" {
		authBase = defaultAuth
	}
	return &ConnectClient{
		clientID:    clientID,
		secretKey:   secretKey,
		redirectURI: redirectURI,
		authBase:    authBase,
		publicKey:   publicKey,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
	}
}

// PublicKey devuelve la public key de la plataforma (usada por el frontend para
// inicializar el SDK de Mercado Pago en operaciones a nivel marketplace).
func (c *ConnectClient) PublicKey() string {
	return c.publicKey
}

// GetAuthorizationURL genera la URL a la que debe redirigirse al vendedor para
// autorizar a la plataforma a operar sus pagos.
func (c *ConnectClient) GetAuthorizationURL(state string) string {
	params := url.Values{}
	params.Set("client_id", c.clientID)
	params.Set("response_type", "code")
	params.Set("platform_id", "mp")
	params.Set("state", state)
	params.Set("redirect_uri", c.redirectURI)

	return c.authBase + "/authorization?" + params.Encode()
}

// doTokenRequest ejecuta un POST a /oauth/token con las credenciales de la aplicación
// y los parámetros del flujo, replicando el comportamiento del cliente de referencia.
func (c *ConnectClient) doTokenRequest(ctx context.Context, form url.Values) (*TokenResponse, error) {
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.secretKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("error creating oauth token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error exchanging mercadopago oauth token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading mercadopago oauth response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("mercadopago oauth token failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result TokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("error unmarshaling mercadopago oauth response: %w", err)
	}

	return &result, nil
}

// ExchangeCode intercambia el authorization_code devuelto por Mercado Pago por
// credenciales de acceso del vendedor.
func (c *ConnectClient) ExchangeCode(ctx context.Context, authorizationCode string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", authorizationCode)
	form.Set("redirect_uri", c.redirectURI)
	return c.doTokenRequest(ctx, form)
}

// RefreshToken renueva el access_token de un vendedor usando su refresh_token.
func (c *ConnectClient) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	return c.doTokenRequest(ctx, form)
}

// GetUserID obtiene el ID de usuario de Mercado Pago asociado al access_token
// del vendedor, para guardar referencia de la cuenta conectada.
func (c *ConnectClient) GetUserID(ctx context.Context, accessToken string) (string, error) {
	cfg, err := mp.New(accessToken)
	if err != nil {
		return "", fmt.Errorf("error creating mercadopago config: %w", err)
	}

	client := user.NewClient(cfg)
	result, err := client.Get(ctx)
	if err != nil {
		return "", fmt.Errorf("error getting mercadopago user: %w", err)
	}

	return fmt.Sprintf("%d", result.ID), nil
}

// RevokeToken revoca el acceso de la plataforma a la cuenta del vendedor.
func (c *ConnectClient) RevokeToken(ctx context.Context, accessToken string) error {
	form := url.Values{}
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.secretKey)
	form.Set("token", accessToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.mercadopago.com/oauth/revoke", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("error creating revoke request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error revoking mercadopago token: %w", err)
	}
	defer resp.Body.Close()

	// Mercado Pago responde 200 aunque el token ya no exista.
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mercado pago revoke failed with status %d", resp.StatusCode)
	}

	return nil
}
