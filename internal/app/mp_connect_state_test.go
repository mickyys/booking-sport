package app

import (
	"context"
	"encoding/base64"
	"net/url"
	"testing"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/mercadopago"
)

func newStateUC() *MPConnectUseCase {
	return &MPConnectUseCase{stateSecret: "secreto-de-prueba"}
}

func TestMPConnectState_SinOrigen_RoundTrip(t *testing.T) {
	uc := newStateUC()
	id, source, err := uc.decodeState(uc.encodeState("abc123", ""))
	if err != nil || id != "abc123" || source != "" {
		t.Errorf("decode = (%q,%q,%v), want (abc123,\"\",nil)", id, source, err)
	}
}

func TestMPConnectState_ConOrigenApp_RoundTrip(t *testing.T) {
	uc := newStateUC()
	state := uc.encodeState("abc123", MPConnectSourceApp)
	id, source, err := uc.decodeState(state)
	if err != nil || id != "abc123" || source != MPConnectSourceApp {
		t.Errorf("decode = (%q,%q,%v), want (abc123,app,nil)", id, source, err)
	}
	if got := uc.DecodeStateSource(state); got != MPConnectSourceApp {
		t.Errorf("DecodeStateSource = %q, want app", got)
	}
	if got, _ := uc.DecodeState(state); got != "abc123" {
		t.Errorf("DecodeState = %q, want abc123", got)
	}
}

func TestMPConnectState_OrigenManipulado_FirmaInvalida(t *testing.T) {
	uc := newStateUC()
	// Un state emitido para la web no puede convertirse en uno de la app.
	raw, _ := base64.RawURLEncoding.DecodeString(uc.encodeState("abc123", ""))
	forged := base64.RawURLEncoding.EncodeToString([]byte("abc123|app" + string(raw[len("abc123"):])))
	if _, _, err := uc.decodeState(forged); err == nil {
		t.Error("state manipulado fue aceptado")
	}
	if got := uc.DecodeStateSource(forged); got != "" {
		t.Errorf("DecodeStateSource = %q, want vacío con firma inválida", got)
	}
}

func TestMPConnectState_Basura_Falla(t *testing.T) {
	uc := newStateUC()
	for _, s := range []string{"", "!!!", base64.RawURLEncoding.EncodeToString([]byte("sin-firma"))} {
		if _, _, err := uc.decodeState(s); err == nil {
			t.Errorf("decodeState(%q) aceptado, want error", s)
		}
	}
}

type stateCenterRepo struct {
	SportCenterRepository
	centers []domain.SportCenter
}

func (r *stateCenterRepo) FindByUserID(ctx context.Context, userID string) ([]domain.SportCenter, error) {
	return r.centers, nil
}

func TestMPConnectAuthorizationURL_OrigenApp_ViajaEnState(t *testing.T) {
	center := domain.SportCenter{}
	uc := NewMPConnectUseCase(
		&stateCenterRepo{centers: []domain.SportCenter{center}},
		mercadopago.NewConnectClient("cid", "secreto-de-prueba", "https://api.test/cb", "https://auth.test", "pk"),
		"secreto-de-prueba",
	)
	for source, want := range map[string]string{MPConnectSourceApp: MPConnectSourceApp, "web": "", "": ""} {
		raw, err := uc.GetAuthorizationURL(context.Background(), "auth0|x", source)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(raw)
		if got := uc.DecodeStateSource(u.Query().Get("state")); got != want {
			t.Errorf("source %q -> %q, want %q", source, got, want)
		}
	}
}
