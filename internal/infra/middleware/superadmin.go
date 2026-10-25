package middleware

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// SuperAdminRole es el rol de Auth0 que habilita el panel de plataforma.
const SuperAdminRole = "superadmin"

// RoleResolver resuelve los roles de un usuario en Auth0. Lo implementa el
// cliente de la Management API.
type RoleResolver interface {
	RoleNames(ctx context.Context, userID string) ([]string, error)
}

// IsSuperAdmin indica si el contexto autenticado trae el rol superadmin.
func IsSuperAdmin(c *gin.Context) bool {
	return hasRole(c, SuperAdminRole)
}

func hasRole(c *gin.Context, role string) bool {
	v, ok := c.Get("user_roles")
	if !ok {
		return false
	}
	roles, ok := v.([]string)
	if !ok {
		return false
	}
	for _, r := range roles {
		if strings.EqualFold(strings.TrimSpace(r), role) {
			return true
		}
	}
	return false
}

// El access token puede no incluir el claim de roles (depende de la Action de
// Auth0). Para no depender de esa configuración se consultan los roles en la
// Management API, con una caché corta para no golpear Auth0 en cada request.
var (
	roleCacheMu sync.Mutex
	roleCache   = map[string]roleCacheEntry{}
)

type roleCacheEntry struct {
	roles   []string
	expires time.Time
}

const (
	roleCacheTTL = 5 * time.Minute
	// roleErrorTTL evita repetir la llamada a Auth0 (y su latencia) cuando la
	// app M2M no puede leer roles.
	roleErrorTTL = 30 * time.Second
)

func resolveRoles(ctx context.Context, resolver RoleResolver, userID string) []string {
	roleCacheMu.Lock()
	if e, ok := roleCache[userID]; ok && time.Now().Before(e.expires) {
		roleCacheMu.Unlock()
		return e.roles
	}
	roleCacheMu.Unlock()

	roles, err := resolver.RoleNames(ctx, userID)
	if err != nil {
		log.Printf("[SUPERADMIN] no se pudieron leer los roles de %s en Auth0: %v", userID, err)
		roleCacheMu.Lock()
		roleCache[userID] = roleCacheEntry{roles: nil, expires: time.Now().Add(roleErrorTTL)}
		roleCacheMu.Unlock()
		return nil
	}
	roleCacheMu.Lock()
	roleCache[userID] = roleCacheEntry{roles: roles, expires: time.Now().Add(roleCacheTTL)}
	roleCacheMu.Unlock()
	return roles
}

func containsRole(roles []string, role string) bool {
	for _, r := range roles {
		if strings.EqualFold(strings.TrimSpace(r), role) {
			return true
		}
	}
	return false
}

// RequireSuperAdmin rechaza con 403 a quien no tenga el rol superadmin. Si el
// token no trae el claim, consulta Auth0 (resolver puede ser nil para
// desactivar ese fallback). Debe ir después del middleware de autenticación.
func RequireSuperAdmin(resolver RoleResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsSuperAdmin(c) {
			c.Next()
			return
		}

		uid, _ := c.Get("user_id")
		userID, _ := uid.(string)
		if resolver != nil && userID != "" {
			roles := resolveRoles(c.Request.Context(), resolver, userID)
			if containsRole(roles, SuperAdminRole) {
				c.Set("user_roles", roles)
				c.Next()
				return
			}
		}

		roles, _ := c.Get("user_roles")
		keys, _ := c.Get("token_claim_keys")
		log.Printf("[SUPERADMIN] acceso denegado user_id=%v roles_parsed=%v claim_keys_en_token=%v (se espera rol %q en un claim terminado en /roles o 'roles', o el fallback a Auth0)", userID, roles, keys, SuperAdminRole)
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "superadmin role required"})
	}
}
