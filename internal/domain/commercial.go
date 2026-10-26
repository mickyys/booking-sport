package domain

import (
	"strings"
	"time"
)

// Modalidades comerciales del centro (docs/refactor-modelo-comercial.md).
const (
	CommercialModePayAtCenter = "PAY_AT_CENTER" // solo reservas, suscripción mensual
	CommercialModePayOnline   = "PAY_ONLINE"    // pago anticipado, comisión por reserva
	CommercialModeNeedsReview = "NEEDS_REVIEW"  // sin evidencia suficiente: revisión administrativa
)

// Estados de la suscripción mensual (modalidad PAY_AT_CENTER).
const (
	SubscriptionNotConfigured = "NOT_CONFIGURED"
	SubscriptionPending       = "PENDING"
	SubscriptionActive        = "ACTIVE"
	SubscriptionPaused        = "PAUSED"
	SubscriptionCancelled     = "CANCELLED"
)

// Estados del proveedor de pago (modalidad PAY_ONLINE).
const (
	ProviderMercadoPago        = "MERCADO_PAGO"
	ProviderStatusNotConnected = "NOT_CONNECTED"
	ProviderStatusConnected    = "CONNECTED"
	ProviderStatusError        = "ERROR"
	ProviderStatusRetired      = "RETIRED"
)

// Origen de la comisión efectiva.
const (
	CommissionSourceDefault = "DEFAULT"
	CommissionSourceCenter  = "CENTER"
)

// Ciclo de vida comercial del centro.
const (
	LifecycleDraft               = "DRAFT"
	LifecycleConfiguring         = "CONFIGURING"
	LifecyclePendingSubscription = "PENDING_SUBSCRIPTION"
	LifecyclePendingPaymentSetup = "PENDING_PAYMENT_SETUP"
	LifecycleReady               = "READY"
	LifecyclePublished           = "PUBLISHED"
	LifecycleSuspended           = "SUSPENDED"
)

const (
	SubscriptionPlanLegacy   = "LEGACY"
	SubscriptionPlanMonthly  = "MONTHLY"
	CommissionTypePercentage = "PERCENTAGE"
)

// Requisitos pendientes devueltos por CanPublishSportCenter.
const (
	PublishMissingBasicInfo       = "basic_info"
	PublishMissingSpaces          = "spaces"
	PublishMissingSchedules       = "schedules"
	PublishMissingPaymentMode     = "payment_mode"
	PublishMissingSubscription    = "subscription"
	PublishMissingMercadoPago     = "mercadopago"
	PublishMissingCommercialCheck = "commercial_review"
)

// Commercial es la configuración comercial de un centro. Es opcional: un
// centro sin `commercial` conserva el comportamiento anterior.
type Commercial struct {
	PaymentMode   string         `bson:"payment_mode,omitempty" json:"payment_mode,omitempty"`
	Subscription  *Subscription  `bson:"subscription,omitempty" json:"subscription,omitempty"`
	OnlinePayment *OnlinePayment `bson:"online_payment,omitempty" json:"online_payment,omitempty"`
	// Published es la publicación explícita. nil = centro histórico: se
	// considera publicado mientras no esté suspendido.
	Published   *bool      `bson:"published,omitempty" json:"published,omitempty"`
	PublishedAt *time.Time `bson:"published_at,omitempty" json:"published_at,omitempty"`
	Migration   string     `bson:"migration,omitempty" json:"migration,omitempty"`
	MigratedAt  *time.Time `bson:"migrated_at,omitempty" json:"migrated_at,omitempty"`
	Review      bool       `bson:"review,omitempty" json:"review,omitempty"`
	Reason      string     `bson:"reason,omitempty" json:"reason,omitempty"`
	// LegacyPaymentModeSet marca que la migración fijó `payment_mode` (para
	// poder revertirlo).
	LegacyPaymentModeSet bool `bson:"legacy_payment_mode_set,omitempty" json:"-"`
}

// Subscription es la suscripción mensual a ReservaloYA.
type Subscription struct {
	Status       string     `bson:"status" json:"status"`
	Plan         string     `bson:"plan,omitempty" json:"plan,omitempty"`
	Amount       float64    `bson:"amount" json:"amount"`
	ProviderRef  string     `bson:"provider_ref,omitempty" json:"-"`
	StartedAt    *time.Time `bson:"started_at,omitempty" json:"started_at,omitempty"`
	NextChargeAt *time.Time `bson:"next_charge_at,omitempty" json:"next_charge_at,omitempty"`
	CancelledAt  *time.Time `bson:"cancelled_at,omitempty" json:"cancelled_at,omitempty"`
}

// OnlinePayment agrupa comisión y proveedor de la modalidad PAY_ONLINE.
type OnlinePayment struct {
	Commission      *CommissionConfig `bson:"commission,omitempty" json:"commission,omitempty"`
	PaymentProvider *PaymentProvider  `bson:"payment_provider,omitempty" json:"payment_provider,omitempty"`
	// RetiredProviders conserva el histórico de proveedores dados de baja
	// (Fintoc); no se usan para cobrar.
	RetiredProviders []PaymentProvider `bson:"retired_providers,omitempty" json:"retired_providers,omitempty"`
}

// CommissionConfig distingue "sin valor" (Value nil) de un 0% explícito.
type CommissionConfig struct {
	Type   string `bson:"type" json:"type"`
	Value  *int   `bson:"value,omitempty" json:"value,omitempty"`
	Source string `bson:"source" json:"source"`
}

type PaymentProvider struct {
	Provider string `bson:"provider" json:"provider"`
	Status   string `bson:"status" json:"status"`
}

// EffectiveCommercialMode devuelve la modalidad vigente: la de `commercial`
// cuando está definida y clasificada, o la legacy (`payment_mode`) mapeada.
func (c *SportCenter) EffectiveCommercialMode() string {
	if c.Commercial != nil {
		switch c.Commercial.PaymentMode {
		case CommercialModePayAtCenter, CommercialModePayOnline:
			return c.Commercial.PaymentMode
		}
	}
	switch c.PaymentMode {
	case "pay_at_center":
		return CommercialModePayAtCenter
	case "pay_online":
		return CommercialModePayOnline
	}
	return ""
}

// IsPublished indica si el centro está publicado. Los centros históricos sin
// publicación explícita se consideran publicados salvo que su onboarding siga
// pendiente.
func (c *SportCenter) IsPublished() bool {
	if c.Commercial != nil && c.Commercial.Published != nil {
		return *c.Commercial.Published
	}
	return c.OnboardingStatus != "pending"
}

// MercadoPagoStatus es el estado real de la conexión (derivado de las
// credenciales, no del valor guardado por la migración).
func (c *SportCenter) MercadoPagoStatus() string {
	if c.MercadoPago.IsConnected() {
		return ProviderStatusConnected
	}
	if c.Commercial != nil && c.Commercial.OnlinePayment != nil && c.Commercial.OnlinePayment.PaymentProvider != nil &&
		c.Commercial.OnlinePayment.PaymentProvider.Status == ProviderStatusError {
		return ProviderStatusError
	}
	return ProviderStatusNotConnected
}

// SubscriptionStatus devuelve el estado de la suscripción del centro.
func (c *SportCenter) SubscriptionStatus() string {
	if c.Commercial == nil || c.Commercial.Subscription == nil || c.Commercial.Subscription.Status == "" {
		return SubscriptionNotConfigured
	}
	return c.Commercial.Subscription.Status
}

// EffectiveCommission resuelve la comisión de una reserva online.
//
// Con el modelo comercial desactivado se conserva el comportamiento histórico:
// `marketplace_fee_percent` del centro si es válido y, si no, el default.
// Con el modelo activo manda `commercial.online_payment.commission` cuando
// tiene un valor explícito (incluido 0%). Si el centro no es PAY_ONLINE no
// aplica comisión.
func (c *SportCenter) EffectiveCommission(enforced bool, defaultRate int) (int, string) {
	if enforced {
		if c.EffectiveCommercialMode() == CommercialModePayAtCenter {
			return 0, CommissionSourceCenter
		}
		if c.Commercial != nil && c.Commercial.OnlinePayment != nil {
			if cm := c.Commercial.OnlinePayment.Commission; cm != nil && cm.Source == CommissionSourceCenter && cm.Value != nil &&
				*cm.Value >= 0 && *cm.Value <= 100 {
				return *cm.Value, CommissionSourceCenter
			}
		}
	}
	if c.MarketplaceFeePercent > 0 && c.MarketplaceFeePercent <= 100 {
		return c.MarketplaceFeePercent, CommissionSourceCenter
	}
	return defaultRate, CommissionSourceDefault
}

// PublishInputs son los datos externos al centro que exige la publicación.
type PublishInputs struct {
	HasSpaces    bool
	HasSchedules bool
}

// PublishCheck es el resultado de validar la publicación.
type PublishCheck struct {
	CanPublish bool     `json:"can_publish"`
	Status     string   `json:"status"`
	Missing    []string `json:"missing"`
}

// CanPublishSportCenter centraliza las reglas de publicación. Es la única
// fuente de verdad: el frontend solo la refleja.
func CanPublishSportCenter(c *SportCenter, in PublishInputs) PublishCheck {
	missing := []string{}

	if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Address) == "" || strings.TrimSpace(c.City) == "" {
		missing = append(missing, PublishMissingBasicInfo)
	}
	if !in.HasSpaces {
		missing = append(missing, PublishMissingSpaces)
	}
	if !in.HasSchedules {
		missing = append(missing, PublishMissingSchedules)
	}

	if c.Commercial != nil && c.Commercial.Review {
		missing = append(missing, PublishMissingCommercialCheck)
	}

	switch c.EffectiveCommercialMode() {
	case CommercialModePayAtCenter:
		if c.SubscriptionStatus() != SubscriptionActive {
			missing = append(missing, PublishMissingSubscription)
		}
	case CommercialModePayOnline:
		if c.MercadoPagoStatus() != ProviderStatusConnected {
			missing = append(missing, PublishMissingMercadoPago)
		}
	default:
		missing = append(missing, PublishMissingPaymentMode)
	}

	check := PublishCheck{CanPublish: len(missing) == 0, Missing: missing}
	check.Status = c.LifecycleStatus(check)
	return check
}

// LifecycleStatus deriva el estado comercial a partir de los datos del centro.
// No se persiste: evita desincronizarse del `status` operativo del superadmin.
func (c *SportCenter) LifecycleStatus(check PublishCheck) string {
	if c.Status == SportCenterStatusSuspended || c.Status == SportCenterStatusBlocked {
		return LifecycleSuspended
	}
	if c.IsPublished() {
		return LifecyclePublished
	}
	if check.CanPublish {
		return LifecycleReady
	}
	if c.OnboardingStatus == "pending" && c.EffectiveCommercialMode() == "" && c.Name == "" {
		return LifecycleDraft
	}
	setupDone := true
	for _, m := range check.Missing {
		if m != PublishMissingSubscription && m != PublishMissingMercadoPago {
			setupDone = false
		}
	}
	if setupDone {
		switch c.EffectiveCommercialMode() {
		case CommercialModePayAtCenter:
			return LifecyclePendingSubscription
		case CommercialModePayOnline:
			return LifecyclePendingPaymentSetup
		}
	}
	return LifecycleConfiguring
}
