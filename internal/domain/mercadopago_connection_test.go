package domain

import (
	"testing"
	"time"
)

func TestEffectiveConnectionType(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		cfg  *MercadoPagoConfig
		want string
	}{
		{name: "nil", cfg: nil, want: ""},
		{name: "vacío", cfg: &MercadoPagoConfig{}, want: ""},
		{
			name: "oauth explícito",
			cfg:  &MercadoPagoConfig{AccessToken: "APP_USR-1", ConnectionType: MPConnectionOAuth},
			want: MPConnectionOAuth,
		},
		{
			name: "manual explícito",
			cfg:  &MercadoPagoConfig{AccessToken: "APP_USR-1", ConnectionType: MPConnectionManual},
			want: MPConnectionManual,
		},
		{
			// Documento anterior a migrate-mp-connection-type: se infiere por el
			// refresh_token.
			name: "sin campo, con refresh_token",
			cfg:  &MercadoPagoConfig{AccessToken: "APP_USR-1", RefreshToken: "TG-1"},
			want: MPConnectionOAuth,
		},
		{
			name: "sin campo, con connected_at",
			cfg:  &MercadoPagoConfig{AccessToken: "APP_USR-1", ConnectedAt: &now},
			want: MPConnectionOAuth,
		},
		{
			name: "sin campo, sólo access_token",
			cfg:  &MercadoPagoConfig{AccessToken: "APP_USR-1"},
			want: MPConnectionManual,
		},
		{
			// Un valor desconocido no se asume: se infiere igual que si faltara.
			name: "valor desconocido",
			cfg:  &MercadoPagoConfig{AccessToken: "APP_USR-1", ConnectionType: "otro"},
			want: MPConnectionManual,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.EffectiveConnectionType(); got != tt.want {
				t.Fatalf("EffectiveConnectionType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSupportsMarketplaceFee(t *testing.T) {
	oauth := &MercadoPagoConfig{AccessToken: "APP_USR-1", ConnectionType: MPConnectionOAuth}
	manual := &MercadoPagoConfig{AccessToken: "APP_USR-1", ConnectionType: MPConnectionManual}
	var nilCfg *MercadoPagoConfig

	if !oauth.SupportsMarketplaceFee() {
		t.Error("oauth debe soportar marketplace_fee")
	}
	if manual.SupportsMarketplaceFee() {
		t.Error("manual no debe soportar marketplace_fee")
	}
	if nilCfg.SupportsMarketplaceFee() {
		t.Error("config nil no debe soportar marketplace_fee")
	}
}
