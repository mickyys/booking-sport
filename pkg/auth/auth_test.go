package auth

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestClaimEmail(t *testing.T) {
	tests := []struct {
		name   string
		claims jwt.MapClaims
		want   string
	}{
		{
			name:   "claim estandar",
			claims: jwt.MapClaims{"email": "catolica1@yopmail.com"},
			want:   "catolica1@yopmail.com",
		},
		{
			name:   "claim con namespace de la Action",
			claims: jwt.MapClaims{"https://reservaloya.cl/email": "catolica1@yopmail.com"},
			want:   "catolica1@yopmail.com",
		},
		{
			name:   "estandar vacio cae al namespaced",
			claims: jwt.MapClaims{"email": "", "https://reservaloya.cl/email": "catolica1@yopmail.com"},
			want:   "catolica1@yopmail.com",
		},
		{
			name:   "sin correo en el token",
			claims: jwt.MapClaims{"sub": "auth0|6af2991fd0bd0cd90b63b53f"},
			want:   "",
		},
		{
			name:   "tipo inesperado se ignora",
			claims: jwt.MapClaims{"email": 42},
			want:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := claimEmail(tc.claims); got != tc.want {
				t.Fatalf("claimEmail() = %q, want %q", got, tc.want)
			}
		})
	}
}
