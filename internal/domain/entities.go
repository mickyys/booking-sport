package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type UserRole string

const (
	RoleAdmin UserRole = "admin"
	RoleUser  UserRole = "user"
)

type Coordinates struct {
	Lat float64 `bson:"lat" json:"lat"`
	Lng float64 `bson:"lng" json:"lng"`
}

// GeoJSONPoint es el formato requerido por los índices 2dsphere de MongoDB.
// Coordinates sigue el orden [longitud, latitud] según la especificación GeoJSON.
type GeoJSONPoint struct {
	Type        string     `bson:"type" json:"type"`
	Coordinates [2]float64 `bson:"coordinates" json:"coordinates"`
}

type Contact struct {
	Phone string `bson:"phone" json:"phone"`
	Email string `bson:"email" json:"email"`
}

type MercadoPagoConfig struct {
	AccessToken  string     `bson:"access_token" json:"-"`
	RefreshToken string     `bson:"refresh_token" json:"-"`
	ExpiresAt    *time.Time `bson:"expires_at" json:"expires_at"`
	MPUserID     string     `bson:"mp_user_id" json:"mp_user_id"`
	PublicKey    string     `bson:"public_key" json:"-"`
	Scope        string     `bson:"scope" json:"-"`
	LiveMode     bool       `bson:"live_mode" json:"live_mode"`
	ConnectedAt  *time.Time `bson:"connected_at" json:"connected_at"`
	UpdatedAt    *time.Time `bson:"updated_at" json:"updated_at"`
}

// IsConnected indica si el centro tiene credenciales válidas (no expiradas) para recibir pagos.
func (c *MercadoPagoConfig) IsConnected() bool {
	if c == nil || c.AccessToken == "" {
		return false
	}
	if c.ExpiresAt != nil && time.Now().After(*c.ExpiresAt) {
		return false
	}
	return true
}

type TimeSlot struct {
	StartTime string `bson:"start_time" json:"start_time"`
	EndTime   string `bson:"end_time" json:"end_time"`
}

type SportCenterBranding struct {
	Name         string `bson:"name,omitempty" json:"name,omitempty"`
	LogoURL      string `bson:"logo_url,omitempty" json:"logo_url,omitempty"`
	PrimaryColor string `bson:"primary_color,omitempty" json:"primary_color,omitempty"`
	PrimaryDark  string `bson:"primary_dark,omitempty" json:"primary_dark,omitempty"`
	Accent       string `bson:"accent,omitempty" json:"accent,omitempty"`
	NavBg        string `bson:"nav_bg,omitempty" json:"nav_bg,omitempty"`
	HeroGradient string `bson:"hero_gradient,omitempty" json:"hero_gradient,omitempty"`
}

type SportCenter struct {
	ID   primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Slug string             `bson:"slug" json:"slug"`
	Name string             `bson:"name" json:"name"`
	City string             `bson:"city" json:"city"`
	// Timezone es la zona IANA del centro (por ejemplo America/Santiago).
	// Gobierna la interpretación de fechas y horas civiles. Vacío en centros
	// históricos: se interpretan como America/Santiago hasta el backfill.
	Timezone string `bson:"timezone,omitempty" json:"timezone,omitempty"`
	// OccupancyAuthority gobierna quién decide la exclusión de horarios del
	// centro: "" u "off" (sólo MongoDB), "shadow" (MongoDB decide, Redis recibe
	// copia y se comparan decisiones) o "authority" (Redis decide). No es
	// editable por la API pública.
	OccupancyAuthority string `bson:"occupancy_authority,omitempty" json:"-"`
	// OccupancyGeneration se publica en MongoDB y Redis en cada corte. Una
	// instancia con generación vieja no puede adquirir horarios.
	OccupancyGeneration int64       `bson:"occupancy_generation,omitempty" json:"-"`
	Address             string      `bson:"address" json:"address"`
	Coordinates         Coordinates `bson:"coordinates" json:"coordinates"`
	// Location es la representación GeoJSON de Coordinates, usada solo para el
	// índice 2dsphere y $geoNear. No se expone en la API pública.
	Location   *GeoJSONPoint `bson:"location,omitempty" json:"-"`
	IsFeatured bool          `bson:"is_featured" json:"is_featured"`
	// FeaturedPriority ordena los destacados (mayor = primero). FeaturedUntil
	// es la vigencia opcional del destacado.
	FeaturedPriority int        `bson:"featured_priority,omitempty" json:"featured_priority,omitempty"`
	FeaturedUntil    *time.Time `bson:"featured_until,omitempty" json:"featured_until,omitempty"`
	// Status lo gobierna el superadmin: vacío o "active" opera normal;
	// "suspended" y "blocked" ocultan el centro y impiden nuevas reservas.
	Status                string               `bson:"status,omitempty" json:"status,omitempty"`
	StatusReason          string               `bson:"status_reason,omitempty" json:"status_reason,omitempty"`
	StatusChangedAt       *time.Time           `bson:"status_changed_at,omitempty" json:"status_changed_at,omitempty"`
	StatusChangedBy       string               `bson:"status_changed_by,omitempty" json:"-"`
	Distance              float64              `bson:"distance,omitempty" json:"distance,omitempty"`
	DistanceKm            float64              `bson:"distance_km,omitempty" json:"distance_km,omitempty"`
	ImageURL              string               `bson:"image_url" json:"image_url"`
	Services              []string             `bson:"services" json:"services"`
	Contact               Contact              `bson:"contact" json:"contact"`
	CourtsCount           int                  `bson:"courts_count" json:"courts"`
	MercadoPago           *MercadoPagoConfig   `bson:"mercadopago,omitempty" json:"mp_user_id"`
	CancellationHours     int                  `bson:"cancellation_hours" json:"cancellation_hours"`
	RetentionPercent      int                  `bson:"retention_percent" json:"retention_percent"`
	PartialPaymentEnabled bool                 `bson:"partial_payment_enabled" json:"partial_payment_enabled"`
	PartialPaymentPercent int                  `bson:"partial_payment_percent" json:"partial_payment_percent"`
	MarketplaceFeePercent int                  `bson:"marketplace_fee_percent" json:"marketplace_fee_percent"`
	IsPrivate             bool                 `bson:"is_private" json:"is_private"`
	Users                 []string             `bson:"users" json:"-"`
	Subdomain             string               `bson:"subdomain,omitempty" json:"subdomain,omitempty"`
	Domains               []string             `bson:"domains,omitempty" json:"domains,omitempty"`
	Branding              *SportCenterBranding `bson:"branding,omitempty" json:"branding,omitempty"`
	CreatedAt             time.Time            `bson:"created_at" json:"created_at"`
	UpdatedAt             time.Time            `bson:"updated_at" json:"updated_at"`
	Rut                   string               `bson:"rut" json:"rut"`
	// GooglePlaceID identifica el recinto en Google Maps; se usa para mostrar
	// calificación y reseñas en el detalle público del centro.
	GooglePlaceID string `bson:"google_place_id,omitempty" json:"google_place_id,omitempty"`
	// AdminEmail es el correo del administrador dueño del centro (1 email = 1 centro).
	// Se normaliza (trim + minúsculas) antes de guardar y consultar.
	AdminEmail string `bson:"admin_email" json:"admin_email"`
	// OnboardingStatus indica si el wizard post-login está pendiente o fue
	// completado. Los centros creados vía onboard parten en "pending"; los
	// centros existentes (campo vacío) no se consideran pendientes.
	OnboardingStatus string `bson:"onboarding_status" json:"onboarding_status"`
	// OnboardingStep es la última etapa completada del wizard (1..5), para
	// retomar donde quedó el usuario al reingresar. 0 = ninguna completada.
	OnboardingStep int `bson:"onboarding_step" json:"onboarding_step"`
	// PaymentMode es la modalidad comercial elegida en el wizard (RES-25):
	// "pay_at_center" (solo reservas) o "pay_online" (pago anticipado).
	PaymentMode string `bson:"payment_mode" json:"payment_mode"`
	// FirstMonthFree activa la oferta "primer mes gratis" para nuevos clientes
	// (RES-26).
	FirstMonthFree bool `bson:"first_month_free" json:"first_month_free"`
	// Commercial es el modelo comercial (modalidad, suscripción, comisión).
	// Opcional: si falta, el centro opera como antes. No se serializa en las
	// respuestas públicas del centro (la comisión es información comercial);
	// se expone por los endpoints /admin/commercial y /superadmin.
	Commercial *Commercial `bson:"commercial,omitempty" json:"-"`
	// SpaceTemplates es un borrador transitorio de la configuración de
	// espacios definida en la etapa "Espacios" del wizard (RES-24). Se persiste
	// para retomar el wizard tras una recarga y se materializa en la colección
	// `spaces` (y `schedule_rules` para canchas) al finalizar el onboarding,
	// momento en el que se limpia. No es la fuente de verdad de los espacios
	// del centro: esos viven en la colección `spaces`.
	SpaceTemplates []SpaceTemplate `bson:"space_templates,omitempty" json:"space_templates,omitempty"`
}

// SpaceTemplate es la plantilla de configuración de un tipo de espacio
// definida en el wizard de onboarding (RES-24). Contiene la cantidad y, según
// el tipo, las reglas semanales (canchas: franjas con precio) o los valores de
// arriendo (quincho/sede/piscina). Al finalizar el onboarding se materializa en
// documentos de la colección `spaces` (y `schedule_rules` para canchas).
type SpaceTemplate struct {
	Tipo     string `bson:"tipo" json:"tipo"`
	Cantidad int    `bson:"cantidad" json:"cantidad"`
	Cancha   bool   `bson:"cancha" json:"cancha"`
	// Rules son las reglas semanales para canchas (sin space_id asignado aún).
	Rules []ScheduleRule `bson:"rules,omitempty" json:"rules,omitempty"`
	// Valores de arriendo para facilities (quincho/sede/piscina).
	PriceAllDay   float64 `bson:"price_all_day,omitempty" json:"price_all_day,omitempty"`
	PricePerDay   float64 `bson:"price_per_day,omitempty" json:"price_per_day,omitempty"`
	PricePerMonth float64 `bson:"price_per_month,omitempty" json:"price_per_month,omitempty"`
}

// TipoEspacio es el tipo de espacio de un centro deportivo.
type TipoEspacio string

// Valid values for TipoEspacio of a sport center.
const (
	TipoEspacioCancha  TipoEspacio = "cancha"
	TipoEspacioQuincho TipoEspacio = "quincho"
	TipoEspacioSede    TipoEspacio = "sede"
	TipoEspacioPiscina TipoEspacio = "piscina"
)

// TipoEspacioIsValid reports whether the given value is a valid TipoEspacio.
func (t TipoEspacio) IsValid() bool {
	return t == TipoEspacioCancha ||
		t == TipoEspacioQuincho ||
		t == TipoEspacioSede ||
		t == TipoEspacioPiscina
}

// TenantInfo es la vista pública de un tenant (mapping de hosts/dominios a un centro).
type TenantInfo struct {
	Subdomain     string               `json:"subdomain"`
	Slug          string               `json:"slug"`
	CenterID      primitive.ObjectID   `json:"center_id"`
	Name          string               `json:"name"`
	Branding      *SportCenterBranding `json:"branding,omitempty"`
	CanonicalHost string               `json:"canonical_host"`
}

type FacilityType string

const (
	FacilityTypeQuincho FacilityType = "quincho"
	FacilityTypeSede    FacilityType = "sede"
	FacilityTypePiscina FacilityType = "piscina"
)

// Facility representa un espacio arrendable del centro distinto a una cancha:
// quincho (arriendo por día), sede (arriendo mensual) o piscina (arriendo mensual).
type Facility struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	Type          FacilityType       `bson:"type" json:"type"`
	Name          string             `bson:"name" json:"name"`
	Description   string             `bson:"description" json:"description"`
	ImageURL      string             `bson:"image_url" json:"image_url"`
	Capacity      int                `bson:"capacity" json:"capacity"`
	Area          string             `bson:"area" json:"area"`
	Features      []string           `bson:"features" json:"features"`
	Enabled       bool               `bson:"enabled" json:"enabled"`

	// Quincho
	PriceAllDay float64 `bson:"price_all_day,omitempty" json:"price_all_day,omitempty"`
	OpenHour    int     `bson:"open_hour,omitempty" json:"open_hour,omitempty"`
	CloseHour   int     `bson:"close_hour,omitempty" json:"close_hour,omitempty"`

	// Sede / Piscina
	PricePerDay     float64 `bson:"price_per_day,omitempty" json:"price_per_day,omitempty"`
	PricePerMonth   float64 `bson:"price_per_month,omitempty" json:"price_per_month,omitempty"`
	MinMonths       int     `bson:"min_months,omitempty" json:"min_months,omitempty"`
	MaxMonths       int     `bson:"max_months,omitempty" json:"max_months,omitempty"`
	AvailableMonths []int   `bson:"available_months,omitempty" json:"available_months,omitempty"`
	DepositMonths   int     `bson:"deposit_months,omitempty" json:"deposit_months,omitempty"`

	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}

type SpaceCategory string

const (
	SpaceCategoryCancha  SpaceCategory = "cancha"
	SpaceCategoryQuincho SpaceCategory = "quincho"
	SpaceCategorySede    SpaceCategory = "sede"
	SpaceCategoryPiscina SpaceCategory = "piscina"
)

func (c SpaceCategory) IsFacility() bool {
	return c == SpaceCategoryQuincho || c == SpaceCategorySede || c == SpaceCategoryPiscina
}

func (c SpaceCategory) IsValid() bool {
	return c == SpaceCategoryCancha || c.IsFacility()
}

// Space representa un espacio del centro deportivo categorizable en el panel de
// administración: cancha (reserva por hora), quincho (arriendo por día), sede o
// piscina (arriendo mensual). Es la unión de los modelos previos Court y Facility.
type Space struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	Category      SpaceCategory      `bson:"category" json:"category"`
	Name          string             `bson:"name" json:"name"`
	Description   string             `bson:"description" json:"description"`
	ImageURL      string             `bson:"image_url" json:"image_url"`
	Enabled       bool               `bson:"enabled" json:"enabled"`

	// Cancha
	YPosition int             `bson:"y_position,omitempty" json:"y_position,omitempty"`
	Schedule  []CourtSchedule `bson:"schedule,omitempty" json:"schedule,omitempty"`

	// Facilities (quincho/sede/piscina) — se conserva `type` para compatibilidad
	// con la UI pública existente.
	Type     FacilityType `bson:"type,omitempty" json:"type,omitempty"`
	Capacity int          `bson:"capacity,omitempty" json:"capacity,omitempty"`
	Area     string       `bson:"area,omitempty" json:"area,omitempty"`
	Features []string     `bson:"features,omitempty" json:"features,omitempty"`

	// Quincho
	PriceAllDay float64 `bson:"price_all_day,omitempty" json:"price_all_day,omitempty"`
	OpenHour    int     `bson:"open_hour,omitempty" json:"open_hour,omitempty"`
	CloseHour   int     `bson:"close_hour,omitempty" json:"close_hour,omitempty"`

	// Sede / Piscina
	PricePerDay     float64 `bson:"price_per_day,omitempty" json:"price_per_day,omitempty"`
	PricePerMonth   float64 `bson:"price_per_month,omitempty" json:"price_per_month,omitempty"`
	MinMonths       int     `bson:"min_months,omitempty" json:"min_months,omitempty"`
	MaxMonths       int     `bson:"max_months,omitempty" json:"max_months,omitempty"`
	AvailableMonths []int   `bson:"available_months,omitempty" json:"available_months,omitempty"`
	DepositMonths   int     `bson:"deposit_months,omitempty" json:"deposit_months,omitempty"`

	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}

func SpaceFromCourt(c *Court) *Space {
	return &Space{
		ID:            c.ID,
		SportCenterID: c.SportCenterID,
		Category:      SpaceCategoryCancha,
		Name:          c.Name,
		Description:   c.Description,
		ImageURL:      c.ImageURL,
		Enabled:       true,
		YPosition:     c.YPosition,
		Schedule:      c.Schedule,
		CreatedAt:     c.CreatedAt,
		UpdatedAt:     c.UpdatedAt,
	}
}

func SpaceToCourt(s *Space) *Court {
	return &Court{
		ID:            s.ID,
		SportCenterID: s.SportCenterID,
		Name:          s.Name,
		Description:   s.Description,
		ImageURL:      s.ImageURL,
		YPosition:     s.YPosition,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
		Schedule:      s.Schedule,
	}
}

func SpaceFromFacility(f *Facility) *Space {
	return &Space{
		ID:              f.ID,
		SportCenterID:   f.SportCenterID,
		Category:        SpaceCategory(f.Type),
		Name:            f.Name,
		Description:     f.Description,
		ImageURL:        f.ImageURL,
		Enabled:         f.Enabled,
		Type:            f.Type,
		Capacity:        f.Capacity,
		Area:            f.Area,
		Features:        f.Features,
		PriceAllDay:     f.PriceAllDay,
		OpenHour:        f.OpenHour,
		CloseHour:       f.CloseHour,
		PricePerDay:     f.PricePerDay,
		PricePerMonth:   f.PricePerMonth,
		MinMonths:       f.MinMonths,
		MaxMonths:       f.MaxMonths,
		AvailableMonths: f.AvailableMonths,
		DepositMonths:   f.DepositMonths,
		CreatedAt:       f.CreatedAt,
		UpdatedAt:       f.UpdatedAt,
	}
}

func SpaceToFacility(s *Space) *Facility {
	return &Facility{
		ID:              s.ID,
		SportCenterID:   s.SportCenterID,
		Type:            FacilityType(s.Category),
		Name:            s.Name,
		Description:     s.Description,
		ImageURL:        s.ImageURL,
		Capacity:        s.Capacity,
		Area:            s.Area,
		Features:        s.Features,
		Enabled:         s.Enabled,
		PriceAllDay:     s.PriceAllDay,
		OpenHour:        s.OpenHour,
		CloseHour:       s.CloseHour,
		PricePerDay:     s.PricePerDay,
		PricePerMonth:   s.PricePerMonth,
		MinMonths:       s.MinMonths,
		MaxMonths:       s.MaxMonths,
		AvailableMonths: s.AvailableMonths,
		DepositMonths:   s.DepositMonths,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
	}
}

// FacilityBookingCodePrefix identifica los códigos de reserva de sede/piscina
// (se comparten con el flujo de pago de Mercado Pago de las canchas).
const FacilityBookingCodePrefix = "FAC-"

// FacilityBooking es la reserva de un día completo de una sede o piscina.
// Una compra de varios días genera un documento por día que comparten
// BookingCode; (space_id, local_date) es único mientras la reserva esté activa,
// garantizando una sola reserva por día.
type FacilityBooking struct {
	ID              primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	BookingCode     string             `bson:"booking_code" json:"booking_code"`
	SpaceID         primitive.ObjectID `bson:"space_id" json:"space_id"`
	SportCenterID   primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	SpaceName       string             `bson:"space_name" json:"space_name"`
	SpaceType       FacilityType       `bson:"space_type" json:"space_type"`
	SportCenterName string             `bson:"sport_center_name" json:"sport_center_name"`
	LocalDate       string             `bson:"local_date" json:"local_date"`
	Guests          int                `bson:"guests" json:"guests"`
	CustomerName    string             `bson:"customer_name" json:"customer_name"`
	CustomerEmail   string             `bson:"customer_email" json:"customer_email"`
	CustomerPhone   string             `bson:"customer_phone" json:"customer_phone"`
	Status          BookingStatus      `bson:"status" json:"status"`
	// Active es true mientras la reserva ocupa el día; respalda el índice único parcial.
	Active bool `bson:"active" json:"-"`
	// PaymentMethod: "mercadopago" (pago online) o "venue" (se paga en el centro).
	PaymentMethod    string  `bson:"payment_method" json:"payment_method"`
	Price            float64 `bson:"price" json:"price"`
	PaidAmount       float64 `bson:"paid_amount" json:"paid_amount"`
	PendingAmount    float64 `bson:"pending_amount" json:"pending_amount"`
	IsPartialPayment bool    `bson:"is_partial_payment" json:"is_partial_payment"`
	// CommissionRate/CommissionAmount: comisión de la plataforma sobre lo cobrado
	// online por este día. Solo aplica al cobro por Mercado Pago.
	CommissionRate   int        `bson:"commission_rate,omitempty" json:"-"`
	CommissionAmount float64    `bson:"commission_amount,omitempty" json:"-"`
	MPPreferenceID   string     `bson:"mp_preference_id,omitempty" json:"-"`
	MPPaymentID      string     `bson:"mp_payment_id,omitempty" json:"-"`
	ExpiresAt        *time.Time `bson:"expires_at,omitempty" json:"expires_at,omitempty"`
	CancelledBy      string     `bson:"cancelled_by,omitempty" json:"cancelled_by,omitempty"`
	CreatedAt        time.Time  `bson:"created_at" json:"created_at"`
	UpdatedAt        time.Time  `bson:"updated_at" json:"updated_at"`
}

// IsActive indica si la reserva ocupa el día (pendiente de pago o confirmada).
func (b *FacilityBooking) IsActive() bool {
	return b.Status == BookingStatusPending || b.Status == BookingStatusConfirmed
}

// FacilityInquiry es una solicitud de arriendo de una facility (lead de contacto).
type FacilityInquiry struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	FacilityID    primitive.ObjectID `bson:"facility_id" json:"facility_id"`
	FacilityName  string             `bson:"facility_name" json:"facility_name"`
	Type          FacilityType       `bson:"type" json:"type"`
	ContactName   string             `bson:"contact_name" json:"contact_name"`
	ContactEmail  string             `bson:"contact_email" json:"contact_email"`
	ContactPhone  string             `bson:"contact_phone" json:"contact_phone"`
	Message       string             `bson:"message,omitempty" json:"message,omitempty"`
	Month         int                `bson:"month,omitempty" json:"month,omitempty"`
	Days          []int              `bson:"days,omitempty" json:"days,omitempty"`
	// Dates (YYYY-MM-DD) permite solicitar días sueltos de distintos meses.
	Dates      []string  `bson:"dates,omitempty" json:"dates,omitempty"`
	Date       string    `bson:"date,omitempty" json:"date,omitempty"`
	SlotKind   string    `bson:"slot_kind,omitempty" json:"slot_kind,omitempty"`
	TotalPrice float64   `bson:"total_price,omitempty" json:"total_price,omitempty"`
	Status     string    `bson:"status" json:"status"`
	CreatedAt  time.Time `bson:"created_at" json:"created_at"`
}

type Court struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	Name          string             `bson:"name" json:"name"`
	Description   string             `bson:"description" json:"description"`
	ImageURL      string             `bson:"image_url" json:"image_url"`
	YPosition     int                `bson:"y_position" json:"y_position"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at" json:"updated_at"`
	Schedule      []CourtSchedule    `bson:"schedule" json:"schedule"`
	// Category y PricePerDay se completan al resolver un espacio no-cancha
	// (quincho/sede/piscina) como unidad reservable. Vacío = cancha.
	Category    SpaceCategory `bson:"category,omitempty" json:"category,omitempty"`
	PricePerDay float64       `bson:"price_per_day,omitempty" json:"-"`
	PriceAllDay float64       `bson:"price_all_day,omitempty" json:"-"`
}

// DayPrice es el precio de arrendar la facility por un día completo: sedes y
// piscinas lo guardan en price_per_day y los quinchos en price_all_day.
func (c *Court) DayPrice() float64 {
	if c.PricePerDay > 0 {
		return c.PricePerDay
	}
	return c.PriceAllDay
}

// IsFacility indica si el espacio reservable es un quincho, sede o piscina
// (reserva de día completo) en lugar de una cancha por hora.
func (c *Court) IsFacility() bool {
	return c != nil && c.Category.IsFacility()
}

type CourtSchedule struct {
	Hour                  int     `bson:"hour" json:"hour"`
	Minutes               int     `bson:"minutes" json:"minutes"`
	Price                 float64 `bson:"price" json:"price"`
	Status                string  `bson:"status" json:"status"`
	PaymentRequired       bool    `bson:"payment_required" json:"payment_required"`
	PaymentOptional       bool    `bson:"payment_optional" json:"payment_optional"`
	PartialPaymentEnabled *bool   `bson:"partial_payment_enabled" json:"partial_payment_enabled"`
	DayOfWeek             *int    `bson:"day_of_week" json:"day_of_week"`
}

// ScheduleRuleSlot es una franja horaria con precio fijo dentro de una regla
// semanal. El precio es fijo por franja, independiente de la duración.
type ScheduleRuleSlot struct {
	StartTime             string  `bson:"start_time" json:"start_time"` // "HH:MM"
	EndTime               string  `bson:"end_time" json:"end_time"`     // "HH:MM"
	Price                 float64 `bson:"price" json:"price"`
	PaymentRequired       bool    `bson:"payment_required" json:"payment_required"`
	PaymentOptional       bool    `bson:"payment_optional" json:"payment_optional"`
	PartialPaymentEnabled *bool   `bson:"partial_payment_enabled,omitempty" json:"partial_payment_enabled,omitempty"`
}

// ScheduleRule define la disponibilidad semanal habitual de un espacio.
// Un espacio puede tener varias reglas (ej. una para lunes-viernes y otra
// para sábado). Para una fecha, la regla más específica gana.
type ScheduleRule struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SpaceID       primitive.ObjectID `bson:"space_id" json:"space_id"`
	SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	DaysOfWeek    []int              `bson:"days_of_week" json:"days_of_week"` // 0=domingo..6=sábado
	Slots         []ScheduleRuleSlot `bson:"slots" json:"slots"`
	Active        bool               `bson:"active" json:"active"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at" json:"updated_at"`
}

// Tipos de excepción de horario.
const (
	ScheduleExceptionClosed  = "closed"
	ScheduleExceptionSpecial = "special"
)

// ScheduleException es una excepción de horario para una fecha concreta.
// Tiene prioridad sobre las reglas semanales.
type ScheduleException struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SpaceID       primitive.ObjectID `bson:"space_id" json:"space_id"`
	SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	Date          time.Time          `bson:"date" json:"date"`
	Type          string             `bson:"type" json:"type"` // closed | special
	Slots         []ScheduleRuleSlot `bson:"slots,omitempty" json:"slots,omitempty"`
	Note          string             `bson:"note,omitempty" json:"note,omitempty"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at" json:"updated_at"`
}

// Block es un bloqueo puntual de una parte de la agenda de un espacio
// (ej. mantenimiento). No modifica la configuración semanal.
type Block struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SpaceID       primitive.ObjectID `bson:"space_id" json:"space_id"`
	SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	Date          time.Time          `bson:"date" json:"date"`
	StartTime     string             `bson:"start_time" json:"start_time"`
	EndTime       string             `bson:"end_time" json:"end_time"`
	Reason        string             `bson:"reason,omitempty" json:"reason,omitempty"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at" json:"updated_at"`
}

type User struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Username  string             `bson:"username" json:"username"`
	Password  string             `bson:"password" json:"-"`
	Role      UserRole           `bson:"role" json:"role"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
}

type UserDevice struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	UserID         string             `bson:"user_id" json:"user_id"`
	SportCenterID  string             `bson:"sport_center_id" json:"sport_center_id"`
	FCMToken       string             `bson:"fcm_token" json:"fcm_token"`
	Platform       string             `bson:"platform" json:"platform"` // ios, android, web
	DeviceName     string             `bson:"device_name" json:"device_name"`
	OSVersion      string             `bson:"os_version" json:"os_version"`
	LastActivityAt time.Time          `bson:"last_activity_at" json:"last_activity_at"`
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
}

// UserProfile guarda datos de contacto de un usuario autenticado (identificado
// por el sub de Auth0) para precargarlos en futuras reservas.
type UserProfile struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Auth0ID   string             `bson:"auth0_id" json:"auth0_id"`
	Phone     string             `bson:"phone" json:"phone"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at" json:"updated_at"`
}

type PagedResponse struct {
	Data       interface{} `json:"data"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	Limit      int         `json:"limit"`
	TotalPages int         `json:"total_pages"`
	// true cuando el filtro por radio no dejó registros y se retornó
	// la cantidad por defecto (los más cercanos) sin corte de distancia.
	UsedRadiusFallback bool `json:"used_radius_fallback,omitempty"`
}

type BookingSummary struct {
	ID                 primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	SportCenterName    string             `bson:"sport_center_name" json:"sport_center_name"`
	CustomerName       string             `bson:"customer_name" json:"customer_name"`
	CustomerPhone      string             `bson:"customer_phone" json:"customer_phone"`
	CustomerEmail      string             `bson:"customer_email" json:"customer_email"`
	BookingCode        string             `bson:"booking_code" json:"booking_code"`
	Date               time.Time          `bson:"date" json:"date"`
	LocalDate          string             `bson:"local_date,omitempty" json:"local_date,omitempty"`
	Timezone           string             `bson:"timezone,omitempty" json:"timezone,omitempty"`
	ScheduledAt        time.Time          `bson:"scheduled_at,omitempty" json:"scheduled_at,omitempty"`
	EndsAt             time.Time          `bson:"ends_at,omitempty" json:"ends_at,omitempty"`
	DurationMinutes    int                `bson:"duration_minutes,omitempty" json:"duration_minutes,omitempty"`
	Hour               int                `bson:"hour" json:"hour"`
	Minutes            int                `bson:"minutes" json:"minutes"`
	CourtName          string             `bson:"court_name" json:"court_name"`
	Status             BookingStatus      `bson:"status" json:"status"`
	Price              float64            `bson:"price" json:"price"`
	FinalPrice         float64            `bson:"final_price" json:"final_price"`
	PaidAmount         float64            `bson:"paid_amount" json:"paid_amount"`
	PendingAmount      float64            `bson:"pending_amount" json:"pending_amount"`
	IsPartialPayment   bool               `bson:"is_partial_payment" json:"is_partial_payment"`
	PartialPaymentPaid bool               `bson:"partial_payment_paid" json:"partial_payment_paid"`
	PaymentMethod      string             `bson:"payment_method" json:"payment_method"`
	CancellationHours  int                `bson:"cancellation_hours" json:"cancellation_hours"`
	RetentionPercent   int                `bson:"retention_percent" json:"retention_percent"`
	CreatedAt          time.Time          `bson:"created_at" json:"created_at"`
}

type RecurringSeriesStatus string

const (
	RecurringSeriesStatusActive   RecurringSeriesStatus = "active"
	RecurringSeriesStatusFinished RecurringSeriesStatus = "finished"
)

type RecurringSeries struct {
	SeriesID          string                `bson:"_id,omitempty" json:"series_id"`
	CustomerName      string                `bson:"customer_name" json:"customer_name"`
	CustomerPhone     string                `bson:"customer_phone" json:"customer_phone"`
	CourtID           primitive.ObjectID    `bson:"court_id,omitempty" json:"court_id"`
	CourtName         string                `bson:"court_name" json:"court_name"`
	SportCenterID     primitive.ObjectID    `bson:"sport_center_id,omitempty" json:"sport_center_id"`
	Hour              int                   `bson:"hour" json:"hour"`
	StartDate         time.Time             `bson:"start_date" json:"start_date"`
	EndDate           time.Time             `bson:"end_date" json:"end_date"`
	FinishedAt        *time.Time            `bson:"finished_at,omitempty" json:"finished_at,omitempty"`
	BookingsCount     int                   `bson:"bookings_count" json:"bookings_count"`
	Price             float64               `bson:"price" json:"price"`
	TotalBookings     int                   `bson:"total_bookings" json:"total_bookings"`
	ConfirmedBookings int                   `bson:"confirmed_bookings" json:"confirmed_bookings"`
	Status            RecurringSeriesStatus `bson:"status" json:"status"`
}

type AdminDashboardData struct {
	TodayBookingsCount int              `json:"today_bookings_count"`
	TodayRevenue       float64          `json:"today_revenue"`
	TodayOnlineRevenue float64          `json:"today_online_revenue"`
	TodayVenueRevenue  float64          `json:"today_venue_revenue"`
	TotalRevenue       float64          `json:"total_revenue"`
	TotalOnlineRevenue float64          `json:"total_online_revenue"`
	TotalVenueRevenue  float64          `json:"total_venue_revenue"`
	CancelledCount     int              `json:"cancelled_count"`
	RecentBookings     []BookingSummary `json:"recent_bookings"`
	TotalRecentCount   int64            `json:"total_recent_count"`
	Page               int              `json:"page"`
	Limit              int              `json:"limit"`
	TotalPages         int              `json:"total_pages"`
}

type BookingStatus string

const (
	BookingStatusPending   BookingStatus = "pending"
	BookingStatusConfirmed BookingStatus = "confirmed"
	BookingStatusCancelled BookingStatus = "cancelled"
	BookingStatusExpired   BookingStatus = "expired"
)

type Booking struct {
	ID              primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	CourtID         primitive.ObjectID `bson:"court_id" json:"court_id"`
	SportCenterID   primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	CourtName       string             `bson:"court_name,omitempty" json:"court_name,omitempty"`
	SportCenterName string             `bson:"sport_center_name,omitempty" json:"sport_center_name,omitempty"`
	UserID          string             `bson:"user_id,omitempty" json:"user_id,omitempty"`
	GuestDeviceID   string             `bson:"guest_device_id,omitempty" json:"guest_device_id,omitempty"`
	GuestDetails    *GuestDetails      `bson:"guest_details,omitempty" json:"guest_details,omitempty"`
	Date            time.Time          `bson:"date" json:"date"`
	LocalDate       string             `bson:"local_date,omitempty" json:"local_date,omitempty"`
	Timezone        string             `bson:"timezone,omitempty" json:"timezone,omitempty"`
	ScheduledAt     time.Time          `bson:"scheduled_at,omitempty" json:"scheduled_at,omitempty"`
	// EndsAt es el fin exclusivo del intervalo, en UTC. Derivado de
	// scheduled_at + duration_minutes en documentos v2.
	EndsAt time.Time `bson:"ends_at,omitempty" json:"ends_at,omitempty"`
	// ScheduleSchemaVersion identifica el contrato temporal con el que se
	// escribió el documento. 0/ausente = legacy (date + hour + minutes),
	// 2 = UTC canónico (scheduled_at + ends_at).
	ScheduleSchemaVersion int `bson:"schedule_schema_version,omitempty" json:"schedule_schema_version,omitempty"`
	// OccupancyID/OccupancyToken vinculan la reserva con su intervalo en el
	// coordinador de ocupación. El token es secreto: nunca se serializa a JSON.
	OccupancyID    string `bson:"occupancy_id,omitempty" json:"-"`
	OccupancyToken string `bson:"occupancy_token,omitempty" json:"-"`
	Hour           int    `bson:"hour" json:"hour"`
	Minutes        int    `bson:"minutes" json:"minutes"`
	// DurationMinutes es la duración de la reserva en minutos. 0 o 60 implica 1 hora.
	DurationMinutes    int           `bson:"duration_minutes,omitempty" json:"duration_minutes,omitempty"`
	FinalPrice         float64       `bson:"final_price" json:"final_price"`
	Price              float64       `bson:"price" json:"price"`
	PaidAmount         float64       `bson:"paid_amount" json:"paid_amount"`
	PendingAmount      float64       `bson:"pending_amount" json:"pending_amount"`
	IsPartialPayment   bool          `bson:"is_partial_payment" json:"is_partial_payment"`
	PartialPaymentPaid bool          `bson:"partial_payment_paid" json:"partial_payment_paid"`
	Status             BookingStatus `bson:"status" json:"status"`
	BookingCode        string        `bson:"booking_code,omitempty" json:"booking_code,omitempty"`
	PaymentMethod      string        `bson:"payment_method,omitempty" json:"payment_method,omitempty"`
	PaymentID          string        `bson:"payment_id,omitempty" json:"payment_id,omitempty"`
	MPPreferenceID     string        `bson:"mp_preference_id,omitempty" json:"mp_preference_id,omitempty"`
	MPPaymentID        string        `bson:"mp_payment_id,omitempty" json:"mp_payment_id,omitempty"`
	PaymentInfo        *PaymentInfo  `bson:"payment_info,omitempty" json:"payment_info,omitempty"`
	// MPStatus es el último estado de pago observado en Mercado Pago.
	MPStatus                string     `bson:"mp_status,omitempty" json:"mp_status,omitempty"`
	LastPaymentSyncAt       *time.Time `bson:"last_payment_sync_at,omitempty" json:"last_payment_sync_at,omitempty"`
	ConfirmationEmailSentAt *time.Time `bson:"confirmation_email_sent_at,omitempty" json:"-"`
	// Pricing es el snapshot inmutable de precios (precio lista, descuentos y
	// comisión) aplicado al crear la reserva. No se recalcula después.
	Pricing *BookingPricing `bson:"pricing,omitempty" json:"pricing,omitempty"`
	// MemberToken y DiscountCode son entradas transitorias del checkout: se
	// usan para cotizar y reservar, pero nunca se persisten en la reserva.
	MemberToken  string `bson:"-" json:"member_token,omitempty"`
	DiscountCode string `bson:"-" json:"discount_code,omitempty"`
	// MemberID solo se acepta en el flujo administrativo (reserva creada por
	// el organizador). El handler público lo ignora.
	MemberID string `bson:"-" json:"member_id,omitempty"`
	// AdminSource marca que la reserva se creó desde el panel. No se persiste.
	AdminSource        bool                  `bson:"-" json:"-"`
	Refunds            []Refund              `bson:"refunds,omitempty" json:"refunds,omitempty"`
	CancelledBy        string                `bson:"cancelled_by,omitempty" json:"cancelled_by,omitempty"`
	CancellationReason string                `bson:"cancellation_reason,omitempty" json:"cancellation_reason,omitempty"`
	CustomerName       string                `bson:"customer_name,omitempty" json:"customer_name,omitempty"`
	CustomerPhone      string                `bson:"customer_phone,omitempty" json:"customer_phone,omitempty"`
	SeriesID           string                `bson:"series_id,omitempty" json:"series_id,omitempty"`
	SeriesStatus       RecurringSeriesStatus `bson:"series_status,omitempty" json:"series_status,omitempty"`
	SeriesFinishedAt   *time.Time            `bson:"series_finished_at,omitempty" json:"series_finished_at,omitempty"`
	SeriesFinishedBy   string                `bson:"series_finished_by,omitempty" json:"series_finished_by,omitempty"`
	SeriesFinishReason string                `bson:"series_finish_reason,omitempty" json:"series_finish_reason,omitempty"`
	RecurringID        string                `bson:"recurring_id,omitempty" json:"recurring_id,omitempty"`
	ModifiedBy         string                `bson:"modified_by,omitempty" json:"modified_by,omitempty"`
	ModifiedAt         *time.Time            `bson:"modified_at,omitempty" json:"modified_at,omitempty"`
	HoldID             *primitive.ObjectID   `bson:"hold_id,omitempty" json:"hold_id,omitempty"`
	LockExpiresAt      *time.Time            `bson:"lock_expires_at,omitempty" json:"lock_expires_at,omitempty"`
	Version            int                   `bson:"version" json:"version"`
	ExpiredAt          *time.Time            `bson:"expired_at,omitempty" json:"expired_at,omitempty"`
	// SpaceCategory es la categoría del espacio al reservar ("" = cancha).
	SpaceCategory SpaceCategory `bson:"space_category,omitempty" json:"space_category,omitempty"`
	// Guests es la cantidad de personas (sedes/piscinas).
	Guests int `bson:"guests,omitempty" json:"guests,omitempty"`
	// GroupCode agrupa los días de una misma compra multi-día de una facility
	// (cada día conserva su propio booking_code único).
	GroupCode string    `bson:"group_code,omitempty" json:"group_code,omitempty"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}

func (b *Booking) NormalizeSchedule() error {
	date, localDate, timezone, scheduledAt, err := NormalizeSchedule(b.Date, b.Hour, b.Minutes)
	if err != nil {
		return err
	}
	b.Date = date
	b.LocalDate = localDate
	b.Timezone = timezone
	b.ScheduledAt = scheduledAt
	return nil
}

type PaymentInfo struct {
	CardLastFour      string  `bson:"card_last_four,omitempty" json:"card_last_four,omitempty"`
	CardholderName    string  `bson:"cardholder_name,omitempty" json:"cardholder_name,omitempty"`
	PaymentMethodID   string  `bson:"payment_method_id,omitempty" json:"payment_method_id,omitempty"`
	PaymentMethodType string  `bson:"payment_method_type,omitempty" json:"payment_method_type,omitempty"`
	Installments      int     `bson:"installments,omitempty" json:"installments,omitempty"`
	InstallmentAmount float64 `bson:"installment_amount,omitempty" json:"installment_amount,omitempty"`
	NetReceivedAmount float64 `bson:"net_received_amount,omitempty" json:"net_received_amount,omitempty"`
	// CommissionRate y CommissionAmount guardan la comisión de ReservaloYA
	// aplicada al monto cobrado en esta transacción. Es histórica: no se
	// recalcula si el centro cambia su porcentaje.
	CommissionRate   int     `bson:"commission_rate,omitempty" json:"commission_rate,omitempty"`
	CommissionAmount float64 `bson:"commission_amount,omitempty" json:"commission_amount,omitempty"`
}

type MockPaymentResult struct {
	Status      string `json:"status"`
	PaymentID   string `json:"payment_id"`
	BookingCode string `json:"booking_code"`
}

type Refund struct {
	ID        string    `bson:"id" json:"id"`
	Amount    int       `bson:"amount" json:"amount"`
	Status    string    `bson:"status" json:"status"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
}

type GuestDetails struct {
	Name  string `bson:"name" json:"name"`
	Email string `bson:"email" json:"email"`
	Phone string `bson:"phone" json:"phone"`
}

type RecurringReservationStatus string

const (
	RecurringReservationStatusActive    RecurringReservationStatus = "active"
	RecurringReservationStatusFinished  RecurringReservationStatus = "finished"
	RecurringReservationStatusCancelled RecurringReservationStatus = "cancelled"
)

type RecurringReservation struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	CourtID       primitive.ObjectID `bson:"court_id" json:"court_id"`
	CustomerName  string             `bson:"customer_name" json:"customer_name"`
	CustomerPhone string             `bson:"customer_phone" json:"customer_phone"`
	Hour          int                `bson:"hour" json:"hour"`
	Minutes       int                `bson:"minutes" json:"minutes"`
	// DurationMinutes es la duración de cada reserva de la recurrencia en minutos.
	DurationMinutes int                        `bson:"duration_minutes,omitempty" json:"duration_minutes,omitempty"`
	Price           float64                    `bson:"price" json:"price"`
	DayOfWeek       int                        `bson:"day_of_week" json:"day_of_week"`           // 0=domingo, 1=lunes, ..., 6=sábado
	DayOfWeekName   string                     `bson:"day_of_week_name" json:"day_of_week_name"` // "lunes", "martes", etc.
	Notes           string                     `bson:"notes,omitempty" json:"notes,omitempty"`
	Status          RecurringReservationStatus `bson:"status" json:"status"`
	CancelledBy     string                     `bson:"cancelled_by,omitempty" json:"cancelled_by,omitempty"`
	CancelReason    string                     `bson:"cancel_reason,omitempty" json:"cancel_reason,omitempty"`
	FinishedAt      *time.Time                 `bson:"finished_at,omitempty" json:"finished_at,omitempty"`
	FinishedBy      string                     `bson:"finished_by,omitempty" json:"finished_by,omitempty"`
	FinishReason    string                     `bson:"finish_reason,omitempty" json:"finish_reason,omitempty"`
	CancelledDates  []string                   `bson:"cancelled_dates,omitempty" json:"cancelled_dates,omitempty"`
	// StartDate es la primera fecha civil de la serie (zona America/Santiago).
	// Es la base para calcular las ocurrencias futuras; las recurrencias
	// antiguas sin este campo usan CreatedAt como respaldo.
	StartDate time.Time `bson:"start_date,omitempty" json:"start_date,omitempty"`
	// HorizonWeeks indica cuántas ocurrencias futuras mantiene el generador.
	// 0 se interpreta como el valor por defecto (4).
	HorizonWeeks int `bson:"horizon_weeks,omitempty" json:"horizon_weeks,omitempty"`
	// Estado de la última generación automática de reservas.
	LastGeneratedAt    *time.Time                 `bson:"last_generated_at,omitempty" json:"last_generated_at,omitempty"`
	LastGenerationOK   bool                       `bson:"last_generation_ok" json:"last_generation_ok"`
	LastGeneratedCount int                        `bson:"last_generated_count" json:"last_generated_count"`
	GenerationError    string                     `bson:"generation_error,omitempty" json:"generation_error,omitempty"`
	GenerationIssues   []RecurringGenerationIssue `bson:"generation_issues,omitempty" json:"generation_issues,omitempty"`
	CreatedAt          time.Time                  `bson:"created_at" json:"created_at"`
	UpdatedAt          time.Time                  `bson:"updated_at" json:"updated_at"`
}

// RecurringGenerationIssue describe una fecha que no pudo generar reserva y
// el motivo (conflicto, bloqueo, feriado, pago pendiente, etc.).
type RecurringGenerationIssue struct {
	Date   string `bson:"date" json:"date"`
	Reason string `bson:"reason" json:"reason"`
}

func (r *RecurringReservation) IsDateCancelled(date time.Time) bool {
	dateStr := SantiagoCivilDate(date).Format(civilDateLayout)
	for _, cd := range r.CancelledDates {
		if cd == dateStr {
			return true
		}
	}
	return false
}

// EffectiveStartDate devuelve la primera fecha de la serie. Para recurrencias
// creadas antes de incorporar StartDate se usa CreatedAt como respaldo.
func (r *RecurringReservation) EffectiveStartDate() time.Time {
	if !r.StartDate.IsZero() {
		return SantiagoCivilDate(r.StartDate)
	}
	if !r.CreatedAt.IsZero() {
		return SantiagoCivilDate(r.CreatedAt)
	}
	return time.Time{}
}

// CoversDate indica si la fecha pertenece a la vigencia de la serie. Las
// recurrencias sin StartDate (legacy) cubren cualquier fecha.
func (r *RecurringReservation) CoversDate(date time.Time) bool {
	start := r.EffectiveStartDate()
	if start.IsZero() {
		return true
	}
	return !SantiagoCivilDate(date).Before(start)
}

type RecurringReservationResponse struct {
	ID              primitive.ObjectID         `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID   primitive.ObjectID         `bson:"sport_center_id" json:"sport_center_id"`
	SportCenterName string                     `bson:"sport_center_name,omitempty" json:"sport_center_name,omitempty"`
	CourtID         primitive.ObjectID         `bson:"court_id" json:"court_id"`
	CourtName       string                     `bson:"court_name,omitempty" json:"court_name,omitempty"`
	CustomerName    string                     `bson:"customer_name" json:"customer_name"`
	CustomerPhone   string                     `bson:"customer_phone" json:"customer_phone"`
	Hour            int                        `bson:"hour" json:"hour"`
	Minutes         int                        `bson:"minutes" json:"minutes"`
	DurationMinutes int                        `bson:"duration_minutes,omitempty" json:"duration_minutes,omitempty"`
	DayOfWeek       int                        `bson:"day_of_week" json:"day_of_week"`
	DayOfWeekName   string                     `bson:"day_of_week_name,omitempty" json:"day_of_week_name,omitempty"`
	Price           float64                    `bson:"price" json:"price"`
	Notes           string                     `bson:"notes,omitempty" json:"notes,omitempty"`
	Status          RecurringReservationStatus `bson:"status" json:"status"`
	CancelledBy     string                     `bson:"cancelled_by,omitempty" json:"cancelled_by,omitempty"`
	CancelReason    string                     `bson:"cancel_reason,omitempty" json:"cancel_reason,omitempty"`
	FinishedAt      *time.Time                 `bson:"finished_at,omitempty" json:"finished_at,omitempty"`
	FinishedBy      string                     `bson:"finished_by,omitempty" json:"finished_by,omitempty"`
	FinishReason    string                     `bson:"finish_reason,omitempty" json:"finish_reason,omitempty"`
	CancelledDates  []string                   `bson:"cancelled_dates,omitempty" json:"cancelled_dates,omitempty"`
	StartDate       time.Time                  `bson:"start_date,omitempty" json:"start_date,omitempty"`
	HorizonWeeks    int                        `bson:"horizon_weeks,omitempty" json:"horizon_weeks,omitempty"`

	LastGeneratedAt    *time.Time                 `bson:"last_generated_at,omitempty" json:"last_generated_at,omitempty"`
	LastGenerationOK   bool                       `bson:"last_generation_ok" json:"last_generation_ok"`
	LastGeneratedCount int                        `bson:"last_generated_count" json:"last_generated_count"`
	GenerationError    string                     `bson:"generation_error,omitempty" json:"generation_error,omitempty"`
	GenerationIssues   []RecurringGenerationIssue `bson:"generation_issues,omitempty" json:"generation_issues,omitempty"`

	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}

type SlotHold struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	CourtID     primitive.ObjectID `bson:"court_id" json:"court_id"`
	Date        time.Time          `bson:"date" json:"date"`
	LocalDate   string             `bson:"local_date,omitempty" json:"local_date,omitempty"`
	Timezone    string             `bson:"timezone,omitempty" json:"timezone,omitempty"`
	ScheduledAt time.Time          `bson:"scheduled_at,omitempty" json:"scheduled_at,omitempty"`
	Hour        int                `bson:"hour" json:"hour"`
	Minutes     int                `bson:"minutes" json:"minutes"`
	UserID      string             `bson:"user_id" json:"user_id"`
	BookingID   primitive.ObjectID `bson:"booking_id" json:"booking_id"`
	CreatedAt   time.Time          `bson:"created_at" json:"created_at"`
	ExpiresAt   time.Time          `bson:"expires_at" json:"expires_at"`
}

func (h *SlotHold) NormalizeSchedule() error {
	date, localDate, timezone, scheduledAt, err := NormalizeSchedule(h.Date, h.Hour, h.Minutes)
	if err != nil {
		return err
	}
	h.Date = date
	h.LocalDate = localDate
	h.Timezone = timezone
	h.ScheduledAt = scheduledAt
	return nil
}

const (
	SportCenterStatusActive    = "active"
	SportCenterStatusSuspended = "suspended"
	SportCenterStatusBlocked   = "blocked"
)

// IsOperational indica si el centro puede mostrarse y recibir reservas.
func (c *SportCenter) IsOperational() bool {
	if c.Commercial != nil && c.Commercial.Published != nil && !*c.Commercial.Published {
		return false
	}
	return c.Status == "" || c.Status == SportCenterStatusActive
}
