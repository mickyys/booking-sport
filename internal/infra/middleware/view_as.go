package middleware

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ViewCenterHeader permite a un superadmin consultar el panel de un centro.
const ViewCenterHeader = "X-View-Center-Id"

const viewAsAuditTTL = 15 * time.Minute

// CenterFinder obtiene un centro por id.
type CenterFinder interface {
	FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error)
}

// ViewAsAuditFunc registra que un superadmin consultó un centro.
type ViewAsAuditFunc func(ctx context.Context, actorID, actorEmail, centerID, ip string)

var (
	viewAsAuditMu   sync.Mutex
	viewAsAuditSeen = map[string]time.Time{}
)

func shouldAuditViewAs(actorID, centerID string) bool {
	key := actorID + "|" + centerID
	now := time.Now()
	viewAsAuditMu.Lock()
	defer viewAsAuditMu.Unlock()
	if t, ok := viewAsAuditSeen[key]; ok && now.Sub(t) < viewAsAuditTTL {
		return false
	}
	viewAsAuditSeen[key] = now
	return true
}

// SuperAdminViewAs deja que un superadmin lea (solo GET) los endpoints
// /api/admin/* de un centro enviando X-View-Center-Id. Se hace pasar por un
// usuario del centro para que los handlers resuelvan ese centro sin cambios.
// Debe ir después del middleware de autenticación.
func SuperAdminViewAs(resolver RoleResolver, centers CenterFinder, audit ViewAsAuditFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := strings.TrimSpace(c.GetHeader(ViewCenterHeader))
		if raw == "" || !strings.HasPrefix(c.Request.URL.Path, "/api/admin/") {
			c.Next()
			return
		}

		uid, _ := c.Get("user_id")
		actorID, _ := uid.(string)
		if !isSuperAdminRequest(c, resolver, actorID) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "superadmin role required"})
			return
		}
		if c.Request.Method != http.MethodGet {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "modo solo lectura: el superadmin no puede modificar datos del centro"})
			return
		}

		id, err := primitive.ObjectIDFromHex(raw)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "center_id inválido"})
			return
		}
		center, err := centers.FindByID(c.Request.Context(), id)
		if err != nil || center == nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "centro deportivo no encontrado"})
			return
		}
		if len(center.Users) == 0 {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "el centro no tiene administradores asociados"})
			return
		}

		if audit != nil && shouldAuditViewAs(actorID, center.ID.Hex()) {
			email, _ := c.Get("user_email")
			actorEmail, _ := email.(string)
			audit(c.Request.Context(), actorID, actorEmail, center.ID.Hex(), c.ClientIP())
		}

		c.Set("view_as_superadmin", true)
		c.Set("view_as_actor_id", actorID)
		c.Set("user_id", center.Users[0])
		c.Next()
	}
}

func isSuperAdminRequest(c *gin.Context, resolver RoleResolver, userID string) bool {
	if IsSuperAdmin(c) {
		return true
	}
	if resolver != nil && userID != "" {
		roles := resolveRoles(c.Request.Context(), resolver, userID)
		if containsRole(roles, SuperAdminRole) {
			c.Set("user_roles", roles)
			return true
		}
	}
	return false
}
