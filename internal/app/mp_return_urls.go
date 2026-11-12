package app

import (
	"context"
	"fmt"
	"net/url"
	"os"
)

type mobileClientKey struct{}

// WithMobileClient marca el contexto como originado en la app móvil, para que
// el retorno de Mercado Pago use enlaces que la app pueda abrir (App Links).
func WithMobileClient(ctx context.Context) context.Context {
	return context.WithValue(ctx, mobileClientKey{}, true)
}

func isMobileClient(ctx context.Context) bool {
	v, _ := ctx.Value(mobileClientKey{}).(bool)
	return v
}

// mpBackURLs arma las back_urls de la preferencia. En la web, success y pending
// pasan por el callback del backend (que registra el pago y redirige). En la
// app van directo al frontend: una redirección 302 no abre la app, un enlace
// directo sí.
func mpBackURLs(ctx context.Context, bookingCode string) (success, failure, pending string) {
	urlFrontend := os.Getenv("URL_FRONTEND")
	code := url.QueryEscape(bookingCode)
	failure = fmt.Sprintf("%s/booking/failure?code=%s", urlFrontend, code)
	if isMobileClient(ctx) {
		status := fmt.Sprintf("%s/booking/status?code=%s", urlFrontend, code)
		return status, failure, status
	}
	callback := fmt.Sprintf("%s?code=%s", os.Getenv("URL_MP_CALLBACK"), bookingCode)
	return callback, failure, callback
}
