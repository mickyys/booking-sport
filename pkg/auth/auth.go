package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Jwks contiene las claves públicas de Auth0
type Jwks struct {
	Keys []JSONWebKeys `json:"keys"`
}

type JSONWebKeys struct {
	Kty string   `json:"kty"`
	Kid string   `json:"kid"`
	Use string   `json:"use"`
	N   string   `json:"n"`
	E   string   `json:"e"`
	X5c []string `json:"x5c"`
}

// getPemCert busca la clave pública correspondiente al kid en el JWKS de Auth0
func getPemCert(domain string, token *jwt.Token) (string, error) {
	jwksEndpoint := fmt.Sprintf("https://%s/.well-known/jwks.json", domain)
	cert := ""
	resp, err := http.Get(jwksEndpoint)
	if err != nil {
		return cert, err
	}
	defer resp.Body.Close()

	var jwks Jwks
	err = json.NewDecoder(resp.Body).Decode(&jwks)
	if err != nil {
		return cert, err
	}

	for k := range jwks.Keys {
		if token.Header["kid"] == jwks.Keys[k].Kid {
			cert = "-----BEGIN CERTIFICATE-----\n" + jwks.Keys[k].X5c[0] + "\n-----END CERTIFICATE-----"
		}
	}

	if cert == "" {
		err = errors.New("Unable to find appropriate key.")
	}

	return cert, err
}

func EnsureValidToken(domain, audience string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid Authorization header format"})
			return
		}

		tokenString := parts[1]
		issuer := fmt.Sprintf("https://%s/", domain)

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("Unexpected signing method: %v", token.Header["alg"])
			}

			cert, err := getPemCert(domain, token)
			if err != nil {
				return nil, err
			}

			return jwt.ParseRSAPublicKeyFromPEM([]byte(cert))
		})

		if err != nil || !token.Valid {
			log.Printf("Token validation failed: %v", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid claims"})
			return
		}

		// Validar Issuer
		iss, _ := claims.GetIssuer()
		if iss != issuer {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid issuer"})
			return
		}

		// Validar Audience
		aud, _ := claims.GetAudience()
		found := false
		for _, a := range aud {
			if a == audience {
				found = true
				break
			}
		}
		if !found {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid audience"})
			return
		}

		// Establecer el user_id en el contexto de Gin para que los handlers lo usen
		c.Set("user_id", claims["sub"])
		// Guardar claims completos para usar en handlers
		if name, ok := claims["name"].(string); ok {
			c.Set("user_name", name)
		}
		if email, ok := claims["email"].(string); ok {
			c.Set("user_email", email)
		}
		if picture, ok := claims["picture"].(string); ok {
			c.Set("user_picture", picture)
		}

		// Extraer roles (pueden venir como claim directo "roles" o con namespace de Auth0)
		var roles []string
		if r, ok := claims["roles"].([]interface{}); ok {
			for _, role := range r {
				if roleStr, ok := role.(string); ok {
					roles = append(roles, roleStr)
				}
			}
		} else {
			// Buscar claims con namespace (ej: https://reservaloya.cl/roles)
			for key, val := range claims {
				if strings.HasSuffix(key, "/roles") {
					if r, ok := val.([]interface{}); ok {
						for _, role := range r {
							if roleStr, ok := role.(string); ok {
								roles = append(roles, roleStr)
							}
						}
					}
				}
			}
		}
		c.Set("user_roles", roles)

		c.Next()
	}
}
