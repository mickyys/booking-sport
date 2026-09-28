package mercadopago

import (
	"testing"
)

func TestVerifyWebhookSignature(t *testing.T) {
	secret := "test-secret"
	dataID := "12345"
	requestID := "550e8400-e29b-41d4-a716-446655440000"
	ts := "1707780833"

	manifest := "id:12345;request-id:550e8400-e29b-41d4-a716-446655440000;ts:1707780833;"
	header := "ts=" + ts + ",v1=" + hmacSHA256Hex(secret, manifest)

	if err := VerifyWebhookSignature(secret, dataID, header, requestID); err != nil {
		t.Fatalf("expected valid signature, got error: %v", err)
	}
}

func TestVerifyWebhookSignatureTampered(t *testing.T) {
	secret := "test-secret"
	dataID := "12345"
	ts := "1707780833"

	manifest := "id:12345;request-id:550e8400-e29b-41d4-a716-446655440000;ts:1707780833;"
	header := "ts=" + ts + ",v1=" + hmacSHA256Hex("wrong-secret", manifest)

	if err := VerifyWebhookSignature(secret, dataID, header, "550e8400-e29b-41d4-a716-446655440000"); err == nil {
		t.Fatal("expected signature mismatch error, got nil")
	}
}

func TestVerifyWebhookSignatureWrongRequestID(t *testing.T) {
	secret := "test-secret"
	dataID := "12345"
	ts := "1707780833"

	manifest := "id:12345;request-id:550e8400-e29b-41d4-a716-446655440000;ts:1707780833;"
	header := "ts=" + ts + ",v1=" + hmacSHA256Hex(secret, manifest)

	// Mismo header pero distinto request-id recibido => debe fallar
	if err := VerifyWebhookSignature(secret, dataID, header, "other-request-id"); err == nil {
		t.Fatal("expected signature mismatch error for different request-id, got nil")
	}
}

func TestVerifyWebhookSignatureMissingParts(t *testing.T) {
	secret := "test-secret"

	cases := []struct {
		name      string
		secret    string
		dataID    string
		header    string
		requestID string
	}{
		{"empty secret", "", "123", "ts=1,v1=x", "req"},
		{"missing header", secret, "123", "", "req"},
		{"missing data id", secret, "", "ts=1,v1=x", "req"},
		{"missing ts", secret, "123", "v1=x", "req"},
		{"missing v1", secret, "123", "ts=1", "req"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := VerifyWebhookSignature(tc.secret, tc.dataID, tc.header, tc.requestID); err == nil {
				t.Fatalf("expected error for case %q, got nil", tc.name)
			}
		})
	}
}
