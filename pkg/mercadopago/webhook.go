package mercadopago

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// VerifyWebhookSignature valida la firma `x-signature` que Mercado Pago incluye
// en las notificaciones de webhook, evitando que un tercero forje notificaciones.
//
// Formato del header x-signature:
//
//	x-signature: ts=<unix_timestamp>,v1=<hmac_sha256_hex>
//
// El manifest firmado se construye como:
//
//	id:<data.id>;request-id:<x-request-id>;ts:<timestamp>;
func VerifyWebhookSignature(secret string, dataID string, signatureHeader string, requestID string) error {
	if secret == "" {
		return fmt.Errorf("mercadopago webhook secret not configured")
	}
	if signatureHeader == "" {
		return fmt.Errorf("mercadopago webhook signature header missing")
	}
	if dataID == "" {
		return fmt.Errorf("mercadopago webhook missing data.id")
	}

	params := parseSignatureHeader(signatureHeader)
	ts, ok := params["ts"]
	if !ok {
		return fmt.Errorf("mercadopago webhook signature missing ts")
	}
	provided, ok := params["v1"]
	if !ok {
		return fmt.Errorf("mercadopago webhook signature missing v1")
	}

	manifest := fmt.Sprintf("id:%s;request-id:%s;ts:%s;", dataID, requestID, ts)
	expected := hmacSHA256Hex(secret, manifest)

	if !hmac.Equal([]byte(expected), []byte(provided)) {
		return fmt.Errorf("mercadopago webhook signature mismatch")
	}
	return nil
}

// parseSignatureHeader convierte "ts=1707780833,v1=abc..." en un mapa clave-valor.
func parseSignatureHeader(header string) map[string]string {
	result := map[string]string{}
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			result[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return result
}

func hmacSHA256Hex(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
