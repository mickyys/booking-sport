package domain

import (
	"fmt"
	"math"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// DiscountType define cómo se calcula un descuento.
type DiscountType string

const (
	DiscountTypePercent DiscountType = "PERCENT"
	DiscountTypeFixed   DiscountType = "FIXED"
)

func (t DiscountType) IsValid() bool {
	return t == DiscountTypePercent || t == DiscountTypeFixed
}

// DiscountScopeMode define a qué espacios aplica un beneficio.
type DiscountScopeMode string

const (
	// DiscountScopeCategory aplica a una o más categorías de espacio.
	DiscountScopeCategory DiscountScopeMode = "category"
	// DiscountScopeSpaces aplica a una selección explícita de espacios.
	DiscountScopeSpaces DiscountScopeMode = "spaces"
	// DiscountScopeAll aplica a todos los espacios del centro.
	DiscountScopeAll DiscountScopeMode = "all"
)

func (m DiscountScopeMode) IsValid() bool {
	return m == DiscountScopeCategory || m == DiscountScopeSpaces || m == DiscountScopeAll
}

// DiscountValue es el valor configurado de un descuento.
type DiscountValue struct {
	Type  DiscountType `bson:"type" json:"type"`
	Value float64      `bson:"value" json:"value"`
}

// DiscountScope delimita los espacios alcanzados por un beneficio. Por
// defecto el alcance es la categoría cancha.
type DiscountScope struct {
	Mode       DiscountScopeMode    `bson:"mode" json:"mode"`
	Categories []SpaceCategory      `bson:"categories,omitempty" json:"categories,omitempty"`
	SpaceIDs   []primitive.ObjectID `bson:"space_ids,omitempty" json:"space_ids,omitempty"`
}

// Normalize completa los valores por defecto del alcance (cancha).
func (s *DiscountScope) Normalize() {
	if s.Mode == "" {
		s.Mode = DiscountScopeCategory
	}
	if s.Mode == DiscountScopeCategory && len(s.Categories) == 0 {
		s.Categories = []SpaceCategory{SpaceCategoryCancha}
	}
}

// Covers indica si el alcance incluye un espacio concreto.
func (s DiscountScope) Covers(category SpaceCategory, spaceID primitive.ObjectID) bool {
	switch s.Mode {
	case DiscountScopeAll:
		return true
	case DiscountScopeSpaces:
		for _, id := range s.SpaceIDs {
			if id == spaceID {
				return true
			}
		}
		return false
	default:
		categories := s.Categories
		if len(categories) == 0 {
			categories = []SpaceCategory{SpaceCategoryCancha}
		}
		for _, c := range categories {
			if c == category {
				return true
			}
		}
		return false
	}
}

// DiscountPeriod define la ventana usada para el límite por usuario.
type DiscountPeriod string

const (
	DiscountPeriodTotal     DiscountPeriod = "total"
	DiscountPeriodDay       DiscountPeriod = "day"
	DiscountPeriodWeek      DiscountPeriod = "week"
	DiscountPeriodFortnight DiscountPeriod = "fortnight"
	DiscountPeriodMonth     DiscountPeriod = "month"
	DiscountPeriodYear      DiscountPeriod = "year"
)

func (p DiscountPeriod) IsValid() bool {
	switch p {
	case DiscountPeriodTotal, DiscountPeriodDay, DiscountPeriodWeek, DiscountPeriodFortnight, DiscountPeriodMonth, DiscountPeriodYear:
		return true
	}
	return false
}

// UsageLimit controla cuántas veces se puede usar un beneficio.
// Un valor 0 en MaxUses o MaxUsesPerUser significa "sin límite".
type UsageLimit struct {
	MaxUses        int            `bson:"max_uses" json:"max_uses"`
	MaxUsesPerUser int            `bson:"max_uses_per_user" json:"max_uses_per_user"`
	Period         DiscountPeriod `bson:"period" json:"period"`
}

func (l *UsageLimit) Normalize() {
	if l.Period == "" || !l.Period.IsValid() {
		l.Period = DiscountPeriodTotal
	}
	if l.MaxUses < 0 {
		l.MaxUses = 0
	}
	if l.MaxUsesPerUser < 0 {
		l.MaxUsesPerUser = 0
	}
}

// MembershipPlan es el plan de socio de un centro. El cobro de la membresía
// ocurre fuera de la plataforma; aquí solo se define el beneficio.
type MembershipPlan struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	Name          string             `bson:"name" json:"name"`
	Description   string             `bson:"description,omitempty" json:"description,omitempty"`
	Discount      DiscountValue      `bson:"discount" json:"discount"`
	Scope         DiscountScope      `bson:"scope" json:"scope"`
	UsageLimit    UsageLimit         `bson:"usage_limit" json:"usage_limit"`
	Active        bool               `bson:"active" json:"active"`
	CreatedBy     string             `bson:"created_by,omitempty" json:"created_by,omitempty"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at" json:"updated_at"`
}

// MemberStatus es el estado administrativo de un socio.
type MemberStatus string

const (
	MemberStatusActive    MemberStatus = "active"
	MemberStatusSuspended MemberStatus = "suspended"
	MemberStatusExpired   MemberStatus = "expired"
)

func (s MemberStatus) IsValid() bool {
	return s == MemberStatusActive || s == MemberStatusSuspended || s == MemberStatusExpired
}

// Member es un socio registrado por el organizador del centro. El correo es
// el destinatario del código de verificación y el documento (RUT) junto al
// correo identifican al socio en el checkout.
type Member struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID  primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	PlanID         primitive.ObjectID `bson:"plan_id" json:"plan_id"`
	DocumentNumber string             `bson:"document_number" json:"document_number"`
	Email          string             `bson:"email" json:"email"`
	Name           string             `bson:"name" json:"name"`
	Phone          string             `bson:"phone,omitempty" json:"phone,omitempty"`
	Auth0ID        string             `bson:"auth0_id,omitempty" json:"auth0_id,omitempty"`
	Status         MemberStatus       `bson:"status" json:"status"`
	ValidFrom      *time.Time         `bson:"valid_from,omitempty" json:"valid_from,omitempty"`
	ValidTo        *time.Time         `bson:"valid_to,omitempty" json:"valid_to,omitempty"`
	Notes          string             `bson:"notes,omitempty" json:"notes,omitempty"`
	CreatedBy      string             `bson:"created_by,omitempty" json:"created_by,omitempty"`
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updated_at"`
}

// IsUsable indica si el socio puede usar su beneficio en la fecha dada.
func (m *Member) IsUsable(now time.Time) bool {
	if m.Status != MemberStatusActive {
		return false
	}
	if m.ValidFrom != nil && now.Before(*m.ValidFrom) {
		return false
	}
	if m.ValidTo != nil && now.After(*m.ValidTo) {
		return false
	}
	return true
}

// DiscountCodeStatus es el único estado accionable de un código. Los estados
// visibles (vigente, utilizado, expirado, anulado) se derivan.
type DiscountCodeStatus string

const (
	DiscountCodeStatusActive DiscountCodeStatus = "active"
	DiscountCodeStatusVoided DiscountCodeStatus = "voided"
)

// Estados derivados que consume el panel administrativo.
const (
	DiscountCodeDerivedScheduled = "scheduled"
	DiscountCodeDerivedAvailable = "available"
	DiscountCodeDerivedExhausted = "exhausted"
	DiscountCodeDerivedExpired   = "expired"
	DiscountCodeDerivedVoided    = "voided"
)

// DiscountCode es un código de descuento generado por el organizador.
type DiscountCode struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	Code          string             `bson:"code" json:"code"`
	BatchID       string             `bson:"batch_id,omitempty" json:"batch_id,omitempty"`
	Discount      DiscountValue      `bson:"discount" json:"discount"`
	Scope         DiscountScope      `bson:"scope" json:"scope"`
	UsageLimit    UsageLimit         `bson:"usage_limit" json:"usage_limit"`
	MinAmount     float64            `bson:"min_amount,omitempty" json:"min_amount,omitempty"`
	ValidFrom     *time.Time         `bson:"valid_from,omitempty" json:"valid_from,omitempty"`
	ValidTo       *time.Time         `bson:"valid_to,omitempty" json:"valid_to,omitempty"`
	Status        DiscountCodeStatus `bson:"status" json:"status"`
	UsedCount     int                `bson:"used_count" json:"used_count"`
	CreatedBy     string             `bson:"created_by,omitempty" json:"created_by,omitempty"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at" json:"updated_at"`
}

// DerivedStatus calcula el estado visible del código.
func (c *DiscountCode) DerivedStatus(now time.Time) string {
	if c.Status == DiscountCodeStatusVoided {
		return DiscountCodeDerivedVoided
	}
	if c.ValidFrom != nil && now.Before(*c.ValidFrom) {
		return DiscountCodeDerivedScheduled
	}
	if c.ValidTo != nil && now.After(*c.ValidTo) {
		return DiscountCodeDerivedExpired
	}
	if c.UsageLimit.MaxUses > 0 && c.UsedCount >= c.UsageLimit.MaxUses {
		return DiscountCodeDerivedExhausted
	}
	return DiscountCodeDerivedAvailable
}

// IsUsable indica si el código se puede aplicar en la fecha dada.
func (c *DiscountCode) IsUsable(now time.Time) bool {
	return c.DerivedStatus(now) == DiscountCodeDerivedAvailable
}

// MemberVerification es el código de un solo uso enviado por correo para
// validar la identidad de un socio.
type MemberVerification struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	MemberID      primitive.ObjectID `bson:"member_id" json:"member_id"`
	CodeHash      string             `bson:"code_hash" json:"-"`
	ExpiresAt     time.Time          `bson:"expires_at" json:"expires_at"`
	Attempts      int                `bson:"attempts" json:"attempts"`
	ConsumedAt    *time.Time         `bson:"consumed_at,omitempty" json:"consumed_at,omitempty"`
	RequestIP     string             `bson:"request_ip,omitempty" json:"-"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
}

// MemberToken es la prueba temporal de que un socio verificó su correo. Se
// guarda hasheado y se envía al cliente solo una vez.
type MemberToken struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	MemberID      primitive.ObjectID `bson:"member_id" json:"member_id"`
	PlanID        primitive.ObjectID `bson:"plan_id" json:"plan_id"`
	TokenHash     string             `bson:"token_hash" json:"-"`
	ExpiresAt     time.Time          `bson:"expires_at" json:"expires_at"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
}

// Fuentes de uso de descuento.
const (
	DiscountSourcePlan = "plan"
	DiscountSourceCode = "code"
)

// Estados de un uso de descuento.
const (
	DiscountUsageReserved  = "reserved"
	DiscountUsageConfirmed = "confirmed"
	DiscountUsageReleased  = "released"
)

// DiscountUsageCounter lleva el conteo atómico por identidad y período para
// hacer cumplir el límite por usuario.
type DiscountUsageCounter struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	Source        string             `bson:"source" json:"source"`
	SourceID      primitive.ObjectID `bson:"source_id" json:"source_id"`
	IdentityKey   string             `bson:"identity_key" json:"identity_key"`
	PeriodKey     string             `bson:"period_key" json:"period_key"`
	Reserved      int                `bson:"reserved" json:"reserved"`
	Confirmed     int                `bson:"confirmed" json:"confirmed"`
	UpdatedAt     time.Time          `bson:"updated_at" json:"updated_at"`
}

// DiscountUsage es el registro auditable de un beneficio aplicado a una
// reserva. Permite responder "quién usó este código".
type DiscountUsage struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID  primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	Source         string             `bson:"source" json:"source"`
	SourceID       primitive.ObjectID `bson:"source_id" json:"source_id"`
	IdentityKey    string             `bson:"identity_key" json:"identity_key"`
	PeriodKey      string             `bson:"period_key,omitempty" json:"period_key,omitempty"`
	BookingID      primitive.ObjectID `bson:"booking_id" json:"booking_id"`
	MemberID       primitive.ObjectID `bson:"member_id,omitempty" json:"member_id,omitempty"`
	Code           string             `bson:"code,omitempty" json:"code,omitempty"`
	Status         string             `bson:"status" json:"status"`
	BaseAmount     float64            `bson:"base_amount" json:"base_amount"`
	DiscountAmount float64            `bson:"discount_amount" json:"discount_amount"`
	FinalAmount    float64            `bson:"final_amount" json:"final_amount"`
	CustomerEmail  string             `bson:"customer_email,omitempty" json:"customer_email,omitempty"`
	CustomerName   string             `bson:"customer_name,omitempty" json:"customer_name,omitempty"`
	CustomerPhone  string             `bson:"customer_phone,omitempty" json:"customer_phone,omitempty"`
	BookingCode    string             `bson:"booking_code,omitempty" json:"booking_code,omitempty"`
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updated_at"`
}

// AppliedDiscount describe el beneficio efectivamente aplicado a una reserva.
type AppliedDiscount struct {
	Kind   string  `bson:"kind" json:"kind"` // membership | code
	ID     string  `bson:"id" json:"id"`
	Code   string  `bson:"code,omitempty" json:"code,omitempty"`
	Label  string  `bson:"label" json:"label"`
	Type   string  `bson:"type" json:"type"`
	Value  float64 `bson:"value" json:"value"`
	Amount float64 `bson:"amount" json:"amount"`
}

// RejectedDiscount explica por qué un beneficio no se aplicó.
type RejectedDiscount struct {
	Kind   string `bson:"kind" json:"kind"`
	Code   string `bson:"code,omitempty" json:"code,omitempty"`
	Reason string `bson:"reason" json:"reason"`
}

// CommissionSnapshot guarda la comisión usada en el momento del cobro.
type CommissionSnapshot struct {
	Rate   int     `bson:"rate" json:"rate"`
	Base   float64 `bson:"base" json:"base"`
	Amount float64 `bson:"amount" json:"amount"`
}

// BookingPricing es el snapshot inmutable de precios de una reserva.
type BookingPricing struct {
	BasePrice     float64             `bson:"base_price" json:"base_price"`
	DiscountTotal float64             `bson:"discount_total" json:"discount_total"`
	FinalPrice    float64             `bson:"final_price" json:"final_price"`
	Applied       []AppliedDiscount   `bson:"applied,omitempty" json:"applied,omitempty"`
	Rejected      []RejectedDiscount  `bson:"rejected,omitempty" json:"rejected,omitempty"`
	MemberID      string              `bson:"member_id,omitempty" json:"member_id,omitempty"`
	CodeID        string              `bson:"code_id,omitempty" json:"code_id,omitempty"`
	Commission    *CommissionSnapshot `bson:"commission,omitempty" json:"commission,omitempty"`
}

// PriceQuote es la cotización que se muestra antes de reservar. No incluye
// datos internos de reserva; el servidor vuelve a cotizar al crear la reserva.
type PriceQuote struct {
	BasePrice     float64            `json:"base_price"`
	DiscountTotal float64            `json:"discount_total"`
	FinalPrice    float64            `json:"final_price"`
	Applied       []AppliedDiscount  `json:"applied"`
	Rejected      []RejectedDiscount `json:"rejected"`
	Member        *QuoteMember       `json:"member,omitempty"`
}

// QuoteMember es la identificación mínima del socio en una cotización.
type QuoteMember struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	PlanID   string `json:"plan_id"`
	PlanName string `json:"plan_name"`
}

// DiscountAmountFor calcula el monto del descuento para un precio base.
func DiscountAmountFor(base float64, d DiscountValue) float64 {
	if base <= 0 {
		return 0
	}
	var amount float64
	switch d.Type {
	case DiscountTypePercent:
		amount = base * d.Value / 100
	case DiscountTypeFixed:
		amount = d.Value
	default:
		return 0
	}
	if amount < 0 {
		amount = 0
	}
	if amount > base {
		amount = base
	}
	return math.Round(amount)
}

// PeriodKey devuelve la clave del período en la zona horaria del centro.
func PeriodKey(period DiscountPeriod, now time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	t := now.In(loc)
	switch period {
	case DiscountPeriodDay:
		return t.Format("2006-01-02")
	case DiscountPeriodWeek:
		year, week := t.ISOWeek()
		return fmt.Sprintf("%d-W%02d", year, week)
	case DiscountPeriodFortnight:
		// Quincena: primera mitad (días 1-15) y segunda mitad (16 al fin de
		// mes) del mes calendario del centro.
		half := "Q1"
		if t.Day() > 15 {
			half = "Q2"
		}
		return fmt.Sprintf("%s-%s", t.Format("2006-01"), half)
	case DiscountPeriodMonth:
		return t.Format("2006-01")
	case DiscountPeriodYear:
		return t.Format("2006")
	default:
		return "all"
	}
}
