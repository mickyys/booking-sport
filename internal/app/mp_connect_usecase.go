package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/logger"
	"github.com/hamp/booking-sport/pkg/mercadopago"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// MPConnectStatus es el estado de la conexión de un centro con Mercado Pago.
type MPConnectStatus struct {
	Connected         bool       `json:"connected"`
	MPUserID          string     `json:"mp_user_id,omitempty"`
	PublicKey         string     `json:"public_key,omitempty"`
	PlatformPublicKey string     `json:"platform_public_key,omitempty"`
	LiveMode          bool       `json:"live_mode"`
	Scope             string     `json:"scope,omitempty"`
	ConnectedAt       *time.Time `json:"connected_at,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	// ConnectionType distingue OAuth (marketplace habilitado) de manual (token
	// compartido, sin marketplace_fee).
	ConnectionType string `json:"connection_type,omitempty"`
}

// MPConnectUseCase implementa el flujo de Mercado Pago Connect (OAuth marketplace):
// el vendedor autoriza a la plataforma a procesar pagos en su nombre.
type MPConnectUseCase struct {
	centerRepo    SportCenterRepository
	connectClient *mercadopago.ConnectClient
	stateSecret   string
}

func NewMPConnectUseCase(centerRepo SportCenterRepository, connectClient *mercadopago.ConnectClient, stateSecret string) *MPConnectUseCase {
	if stateSecret == "" {
		logger.GetLogger().Warnw("mp_connect_state_secret_not_configured",
			"msg", "MP_CLIENT_SECRET no configurado: el parámetro state de OAuth no estará firmado",
		)
	}
	return &MPConnectUseCase{
		centerRepo:    centerRepo,
		connectClient: connectClient,
		stateSecret:   stateSecret,
	}
}

// MPConnectSourceApp marca que la conexión se inició desde la app mobile: al
// terminar el callback devuelve al usuario a la app en vez de a la web.
const MPConnectSourceApp = "app"

// encodeState firma y codifica el ID del centro (y opcionalmente el origen) en
// el parámetro `state` del flujo OAuth. La firma HMAC cubre ambos valores, así
// que un atacante no puede forjar ni reutilizar un state de otro centro ni
// cambiar el destino del redirect (protección contra tampering/CSRF).
func (uc *MPConnectUseCase) encodeState(centerID, source string) string {
	value := centerID
	if source != "" {
		value += "|" + source
	}
	payload := value + "." + uc.signState(value)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// signState calcula un HMAC-SHA256 sobre el valor dado usando el secreto de la
// aplicación (MP_CLIENT_SECRET).
func (uc *MPConnectUseCase) signState(value string) string {
	mac := hmac.New(sha256.New, []byte(uc.stateSecret))
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

// decodeState verifica la firma del state y recupera el ID del centro y el
// origen (vacío si fue la web).
func (uc *MPConnectUseCase) decodeState(state string) (string, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		return "", "", fmt.Errorf("invalid state")
	}
	parts := strings.SplitN(string(raw), ".", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid state")
	}
	value, sig := parts[0], parts[1]
	if !hmac.Equal([]byte(sig), []byte(uc.signState(value))) {
		return "", "", fmt.Errorf("invalid state signature")
	}
	centerID, source, _ := strings.Cut(value, "|")
	return centerID, source, nil
}

// DecodeState expone la decodificación del parámetro `state` del callback para
// que los handlers puedan loguear el center_id involucrado.
func (uc *MPConnectUseCase) DecodeState(state string) (string, error) {
	centerID, _, err := uc.decodeState(state)
	return centerID, err
}

// DecodeStateSource devuelve el origen de la conexión (`app` o vacío) si el
// state es válido.
func (uc *MPConnectUseCase) DecodeStateSource(state string) string {
	_, source, err := uc.decodeState(state)
	if err != nil {
		return ""
	}
	return source
}

// getCenterByUserID resuelve el sport center administrado por el usuario.
func (uc *MPConnectUseCase) getCenterByUserID(ctx context.Context, userID string) (*domain.SportCenter, error) {
	centers, err := uc.centerRepo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("error finding sport centers: %w", err)
	}
	if len(centers) == 0 {
		return nil, fmt.Errorf("no sport center associated with this user")
	}
	return &centers[0], nil
}

// GetAuthorizationURL genera la URL de autorización de Mercado Pago para el
// sport center administrado por el usuario.
func (uc *MPConnectUseCase) GetAuthorizationURL(ctx context.Context, userID, source string) (string, error) {
	center, err := uc.getCenterByUserID(ctx, userID)
	if err != nil {
		return "", err
	}
	if source != MPConnectSourceApp {
		source = ""
	}

	return uc.connectClient.GetAuthorizationURL(uc.encodeState(center.ID.Hex(), source)), nil
}

// HandleCallback procesa el callback de Mercado Pago: intercambia el
// authorization_code por credenciales y las guarda en el centro.
func (uc *MPConnectUseCase) HandleCallback(ctx context.Context, code, state string) (*domain.MercadoPagoConfig, error) {
	log := logger.FromContext(ctx)

	centerIDStr, _, err := uc.decodeState(state)
	if err != nil {
		log.Warnw("mp_connect_callback_invalid_state", "error", err)
		return nil, fmt.Errorf("invalid oauth state")
	}

	id, err := primitive.ObjectIDFromHex(centerIDStr)
	if err != nil {
		log.Warnw("mp_connect_callback_invalid_center_id", "center_id", centerIDStr, "error", err)
		return nil, fmt.Errorf("invalid sport center id in state")
	}

	if code == "" {
		log.Warnw("mp_connect_callback_missing_code", "center_id", id.Hex())
		return nil, fmt.Errorf("missing authorization code")
	}

	token, err := uc.connectClient.ExchangeCode(ctx, code)
	if err != nil {
		log.Errorw("mp_connect_callback_exchange_failed", "center_id", id.Hex(), "error", err)
		return nil, err
	}

	mpUserID := ""
	if mpUserID, err = uc.connectClient.GetUserID(ctx, token.AccessToken); err != nil {
		log.Warnw("mp_connect_callback_get_user_failed", "center_id", id.Hex(), "error", err)
	}

	now := time.Now()
	expiresAt := now.Add(time.Duration(token.ExpiresIn) * time.Second)

	config := &domain.MercadoPagoConfig{
		AccessToken:    token.AccessToken,
		RefreshToken:   token.RefreshToken,
		ExpiresAt:      &expiresAt,
		MPUserID:       mpUserID,
		PublicKey:      token.PublicKey,
		Scope:          token.Scope,
		LiveMode:       token.LiveMode,
		ConnectedAt:    &now,
		UpdatedAt:      &now,
		ConnectionType: domain.MPConnectionOAuth,
	}

	if err := uc.centerRepo.UpdateMercadoPagoConfig(ctx, id, config); err != nil {
		log.Errorw("mp_connect_callback_store_failed", "center_id", id.Hex(), "error", err)
		return nil, fmt.Errorf("error storing mercadopago credentials: %w", err)
	}

	log.Infow("mp_connect_callback_saved", "center_id", id.Hex(), "mp_user_id", mpUserID)
	return config, nil
}

// GetStatus devuelve el estado de conexión del centro con Mercado Pago.
// Si el access_token expiró pero existe un refresh_token, intenta renovarlo.
func (uc *MPConnectUseCase) GetStatus(ctx context.Context, userID string) (*MPConnectStatus, error) {
	center, err := uc.getCenterByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	cfg := center.MercadoPago
	if cfg == nil {
		return &MPConnectStatus{Connected: false}, nil
	}

	if !cfg.IsConnected() && cfg.RefreshToken != "" {
		token, refreshErr := uc.connectClient.RefreshToken(ctx, cfg.RefreshToken)
		if refreshErr == nil {
			now := time.Now()
			expiresAt := now.Add(time.Duration(token.ExpiresIn) * time.Second)
			cfg.AccessToken = token.AccessToken
			cfg.RefreshToken = token.RefreshToken
			cfg.PublicKey = token.PublicKey
			cfg.Scope = token.Scope
			cfg.LiveMode = token.LiveMode
			cfg.ExpiresAt = &expiresAt
			cfg.UpdatedAt = &now
			_ = uc.centerRepo.UpdateMercadoPagoConfig(ctx, center.ID, cfg)
		}
	}

	return &MPConnectStatus{
		Connected:         cfg.IsConnected(),
		MPUserID:          cfg.MPUserID,
		PublicKey:         cfg.PublicKey,
		PlatformPublicKey: uc.connectClient.PublicKey(),
		LiveMode:          cfg.LiveMode,
		Scope:             cfg.Scope,
		ConnectedAt:       cfg.ConnectedAt,
		ExpiresAt:         cfg.ExpiresAt,
		ConnectionType:    cfg.EffectiveConnectionType(),
	}, nil
}

// Disconnect revoca el acceso de la plataforma a la cuenta del vendedor y
// limpia las credenciales guardadas en el centro.
func (uc *MPConnectUseCase) Disconnect(ctx context.Context, userID string) error {
	center, err := uc.getCenterByUserID(ctx, userID)
	if err != nil {
		return err
	}
	return uc.disconnectCenter(ctx, center)
}

// DisconnectByCenterID revoca y elimina las credenciales de Mercado Pago de un
// centro específico. Se usa al desactivar el cobro online después del
// onboarding, donde la operación se origina desde la configuración del centro
// y no desde el usuario autenticado.
func (uc *MPConnectUseCase) DisconnectByCenterID(ctx context.Context, centerID primitive.ObjectID) error {
	center, err := uc.centerRepo.FindByID(ctx, centerID)
	if err != nil {
		return fmt.Errorf("error finding sport center: %w", err)
	}
	return uc.disconnectCenter(ctx, center)
}

// disconnectCenter revoca el token (best-effort) y limpia las credenciales del
// centro. La revocación remota no bloquea la limpieza local: si Mercado Pago
// no responde, el token igualmente deja de usarse desde la plataforma.
func (uc *MPConnectUseCase) disconnectCenter(ctx context.Context, center *domain.SportCenter) error {
	if center.MercadoPago != nil && center.MercadoPago.AccessToken != "" {
		if err := uc.connectClient.RevokeToken(ctx, center.MercadoPago.AccessToken); err != nil {
			logger.FromContext(ctx).Warnw("mp_connect_revoke_token_failed",
				"center_id", center.ID.Hex(),
				"error", err,
			)
		}
	}

	return uc.centerRepo.UpdateMercadoPagoConfig(ctx, center.ID, nil)
}
