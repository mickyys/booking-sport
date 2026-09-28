package app

import (
	"context"
	"os"
	"strings"

	"github.com/hamp/booking-sport/internal/domain"
)

type TenantUseCase struct {
	repo SportCenterRepository
}

func NewTenantUseCase(repo SportCenterRepository) *TenantUseCase {
	return &TenantUseCase{repo: repo}
}

// NormalizeHost normaliza un host: minúsculas, sin puerto y sin punto final.
func NormalizeHost(host string) string {
	h := strings.TrimSpace(host)
	h = strings.ToLower(h)
	h = strings.TrimSuffix(h, ".")
	if i := strings.Index(h, ":"); i >= 0 {
		h = h[:i]
	}
	return h
}

// ResolveByHost resuelve un hostname (subdominio o dominio personalizado de un
// cliente) hacia su tenant. Hace match exacto contra el campo `domains` y, como
// fallback, reintenta sin el prefijo `www.`.
func (uc *TenantUseCase) ResolveByHost(ctx context.Context, host string) (*domain.TenantInfo, error) {
	host = NormalizeHost(host)
	if host == "" {
		return nil, domain.ErrNotFound
	}

	center, err := uc.repo.FindByHost(ctx, host)
	if err == nil {
		return buildTenantInfo(center), nil
	}

	if strings.HasPrefix(host, "www.") {
		center, err = uc.repo.FindByHost(ctx, strings.TrimPrefix(host, "www."))
		if err == nil {
			return buildTenantInfo(center), nil
		}
	}

	return nil, err
}

// ResolveBySubdomain resuelve un tenant por su subdominio canónico (ej: "catolica").
func (uc *TenantUseCase) ResolveBySubdomain(ctx context.Context, subdomain string) (*domain.TenantInfo, error) {
	subdomain = NormalizeHost(subdomain)
	if subdomain == "" {
		return nil, domain.ErrNotFound
	}

	center, err := uc.repo.FindBySubdomain(ctx, subdomain)
	if err != nil {
		return nil, err
	}
	return buildTenantInfo(center), nil
}

// platformDomain devuelve el dominio raíz de la plataforma (ej: reservaloya.cl
// en prod, dev.reservaloya.cl en dev). Configurable vía PLATFORM_DOMAIN.
func platformDomain() string {
	if d := os.Getenv("PLATFORM_DOMAIN"); d != "" {
		return d
	}
	return "reservaloya.cl"
}

func buildTenantInfo(center *domain.SportCenter) *domain.TenantInfo {
	info := &domain.TenantInfo{
		Subdomain: center.Subdomain,
		Slug:      center.Slug,
		CenterID:  center.ID,
		Name:      center.Name,
		Branding:  center.Branding,
	}
	if info.Subdomain != "" {
		info.CanonicalHost = info.Subdomain + "." + platformDomain()
	}
	if info.Branding != nil && info.Branding.Name != "" {
		info.Name = info.Branding.Name
	}
	return info
}
