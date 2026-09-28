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

// encodeState firma y codifica el ID del centro en el parámetro `state` del
// flujo OAuth. La firma HMAC evita que un atacante forje o reutilice un state
// de otro centro (protección contra tampering/CSRF).
func (uc *MPConnectUseCase) encodeState(centerID string) string {
	payload := centerID + "." + uc.signState(centerID)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// signState calcula un HMAC-SHA256 sobre el valor dado usando el secreto de la
// aplicación (MP_CLIENT_SECRET).
func (uc *MPConnectUseCase) signState(value string) string {
	mac := hmac.New(sha256.New, []byte(uc.stateSecret))
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

// decodeState verifica la firma del state y recupera el ID del centro.
func (uc *MPConnectUseCase) decodeState(state string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		return "", fmt.Errorf("invalid state")
	}
	parts := strings.SplitN(string(raw), ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid state")
	}
	centerID, sig := parts[0], parts[1]
	if !hmac.Equal([]byte(sig), []byte(uc.signState(centerID))) {
		return "", fmt.Errorf("invalid state signature")
	}
	return centerID, nil
}

// DecodeState expone la decodificación del parámetro `state` del callback para
// que los handlers puedan loguear el center_id involucrado.
func (uc *MPConnectUseCase) DecodeState(state string) (string, error) {
	return uc.decodeState(state)
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
func (uc *MPConnectUseCase) GetAuthorizationURL(ctx context.Context, userID string) (string, error) {
	center, err := uc.getCenterByUserID(ctx, userID)
	if err != nil {
		return "", err
	}

	return uc.connectClient.GetAuthorizationURL(uc.encodeState(center.ID.Hex())), nil
}

// HandleCallback procesa el callback de Mercado Pago: intercambia el
// authorization_code por credenciales y las guarda en el centro.
func (uc *MPConnectUseCase) HandleCallback(ctx context.Context, code, state string) (*domain.MercadoPagoConfig, error) {
	log := logger.FromContext(ctx)

	centerIDStr, err := uc.decodeState(state)
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
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    &expiresAt,
		MPUserID:     mpUserID,
		PublicKey:    token.PublicKey,
		Scope:        token.Scope,
		LiveMode:     token.LiveMode,
		ConnectedAt:  &now,
		UpdatedAt:    &now,
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
	}, nil
}

// Disconnect revoca el acceso de la plataforma a la cuenta del vendedor y
// limpia las credenciales guardadas en el centro.
func (uc *MPConnectUseCase) Disconnect(ctx context.Context, userID string) error {
	center, err := uc.getCenterByUserID(ctx, userID)
	if err != nil {
		return err
	}

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
