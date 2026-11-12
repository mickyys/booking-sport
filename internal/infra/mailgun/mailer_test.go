package mailgun

import (
	"strings"
	"testing"
)

func renderWelcome(t *testing.T, params welcomeTemplateData) string {
	t.Helper()
	var buf strings.Builder
	if err := welcomeCenterTemplate.Execute(&buf, params); err != nil {
		t.Fatalf("ejecutando plantilla de bienvenida: %v", err)
	}
	return buf.String()
}

func TestWelcomeEmailHTML_Contenido(t *testing.T) {
	params := welcomeTemplateData{
		LogoURL:        logoReservaloYA,
		HomeURL:        "https://app.reservaloya.cl",
		GreetingName:   "Hector",
		Email:          "admin@club.cl",
		CenterName:     "Hector Llolleo Club",
		Address:        "Av. Ossa 123, La Reina",
		TenantURL:      "https://hector-llolleo-club.reservaloya.cl",
		NextStepText:   "Para comenzar, configura tu contraseña y activa tu cuenta.",
		ButtonLabel:    "Configurar mi cuenta",
		ActionURL:      "https://tenant.auth0.com/changepw/xyz",
		ShowExpiration: true,
	}
	html := renderWelcome(t, params)

	cases := []struct {
		nombre string
		frag   string
	}{
		{"logo", logoReservaloYA},
		{"saludo con nombre", "Hola\n                      <strong>Hector</strong>"},
		{"correo en caja", "admin@club.cl"},
		{"nombre centro", "Hector Llolleo Club"},
		{"dirección", "Av. Ossa 123, La Reina"},
		{"url tenant", "https://hector-llolleo-club.reservaloya.cl"},
		{"botón configurar", "Configurar mi cuenta"},
		{"action setup", `href="https://tenant.auth0.com/changepw/xyz"`},
		{"nota de expiración", "Este enlace es personal y de un solo uso."},
		{"footer", "© 2026 ReservaloYA"},
		{"header azul", "#0d1b3a"},
		{"acento verde", "#16e6a1"},
	}
	for _, tc := range cases {
		if !strings.Contains(html, tc.frag) {
			t.Errorf("HTML sin %s: falta %q", tc.nombre, tc.frag)
		}
	}
}

func TestWelcomeEmailHTML_CuentaExistente(t *testing.T) {
	params := welcomeTemplateData{
		LogoURL:        logoReservaloYA,
		HomeURL:        "https://app.reservaloya.cl",
		GreetingName:   "x@y.cl", // sin nombre en Auth0: cae al correo
		Email:          "x@y.cl",
		CenterName:     "Club X",
		Address:        "Dir 1",
		TenantURL:      "https://club-x.reservaloya.cl",
		NextStepText:   "Ingresa al panel para gestionar tus espacios, horarios y reservas.",
		ButtonLabel:    "Ir a mi panel",
		ActionURL:      "https://app.reservaloya.cl/admin",
		ShowExpiration: false,
	}
	html := renderWelcome(t, params)

	if !strings.Contains(html, "Ir a mi panel") {
		t.Error("HTML sin botón 'Ir a mi panel' para cuenta existente")
	}
	if !strings.Contains(html, "Hola\n                      <strong>x@y.cl</strong>") {
		t.Error("HTML sin saludo con correo de fallback")
	}
	if strings.Contains(html, "changepw") {
		t.Error("HTML no debe contener tickets de Auth0 para cuentas existentes")
	}
	if strings.Contains(html, "Este enlace es personal") {
		t.Error("HTML no debe incluir nota de expiración para cuentas existentes")
	}
}
