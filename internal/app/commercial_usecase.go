package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/logger"
	"github.com/hamp/booking-sport/pkg/mercadopago"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// CommercialRepository es la persistencia que necesita el modelo comercial.
type CommercialRepository interface {
	FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error)
	FindByUserID(ctx context.Context, userID string) ([]domain.SportCenter, error)
	// UpdateCommercialFields hace $set de rutas "commercial.*" sin tocar el
	// resto del documento.
	UpdateCommercialFields(ctx context.Context, id primitive.ObjectID, fields map[string]interface{}) error
}

// PaymentModeSetter cambia la modalidad legacy (`payment_mode`) y alinea
// franjas y credenciales, igual que el panel de ajustes.
type PaymentModeSetter interface {
	SetPaymentMode(ctx context.Context, id primitive.ObjectID, mode string) error
}

// SubscriptionProvider crea y consulta suscripciones (Mercado Pago preapproval).
type SubscriptionProvider interface {
	CreateSubscription(ctx context.Context, in mercadopago.SubscriptionInput) (*mercadopago.SubscriptionResult, error)
	GetSubscription(ctx context.Context, id string) (*mercadopago.SubscriptionResult, error)
}

type CommercialConfig struct {
	SubscriptionAmount float64
	SubscriptionBack   string
}

type CommercialUseCase struct {
	repo     CommercialRepository
	spaces   SpaceRepository
	rules    ScheduleRuleRepository
	modes    PaymentModeSetter
	provider SubscriptionProvider
	cfg      CommercialConfig
}

func NewCommercialUseCase(repo CommercialRepository, spaces SpaceRepository, rules ScheduleRuleRepository, modes PaymentModeSetter, provider SubscriptionProvider, cfg CommercialConfig) *CommercialUseCase {
	return &CommercialUseCase{repo: repo, spaces: spaces, rules: rules, modes: modes, provider: provider, cfg: cfg}
}

// CommercialView es la configuración comercial tal como la ve el panel.
type CommercialView struct {
	CenterID     string              `json:"center_id"`
	PaymentMode  string              `json:"payment_mode"`
	Lifecycle    string              `json:"lifecycle_status"`
	Published    bool                `json:"published"`
	Enforced     bool                `json:"enforced"`
	Review       bool                `json:"review"`
	Publish      domain.PublishCheck `json:"publish"`
	Subscription *SubscriptionView   `json:"subscription,omitempty"`
	MercadoPago  string              `json:"mercadopago_status,omitempty"`
	Commission   *CommissionView     `json:"commission,omitempty"`
}

type SubscriptionView struct {
	Status       string     `json:"status"`
	Plan         string     `json:"plan,omitempty"`
	Amount       float64    `json:"amount"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	NextChargeAt *time.Time `json:"next_charge_at,omitempty"`
	CancelledAt  *time.Time `json:"cancelled_at,omitempty"`
}

// CommissionView solo existe para PAY_ONLINE. Effective es lo que se cobra hoy
// (con el modelo desactivado puede diferir de Configured).
type CommissionView struct {
	Effective  int    `json:"effective"`
	Source     string `json:"source"`
	Configured *int   `json:"configured,omitempty"`
}

func (uc *CommercialUseCase) center(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	c, err := uc.repo.FindByID(ctx, id)
	if err != nil || c == nil {
		return nil, domain.NewNotFoundError("centro no encontrado")
	}
	return c, nil
}

// CenterIDForUser resuelve el centro administrado por el usuario autenticado.
func (uc *CommercialUseCase) CenterIDForUser(ctx context.Context, userID string) (primitive.ObjectID, error) {
	centers, err := uc.repo.FindByUserID(ctx, userID)
	if err != nil {
		return primitive.NilObjectID, err
	}
	if len(centers) == 0 {
		return primitive.NilObjectID, domain.NewNotFoundError("el usuario no administra ningún centro")
	}
	return centers[0].ID, nil
}

func (uc *CommercialUseCase) publishInputs(ctx context.Context, c *domain.SportCenter) (domain.PublishInputs, error) {
	spaces, err := uc.spaces.FindByCenterID(ctx, c.ID)
	if err != nil {
		return domain.PublishInputs{}, err
	}
	in := domain.PublishInputs{}
	for _, s := range spaces {
		if s.Enabled {
			in.HasSpaces = true
		}
		if len(s.Schedule) > 0 {
			in.HasSchedules = true
		}
	}
	if !in.HasSchedules && in.HasSpaces && uc.rules != nil {
		rules, err := uc.rules.FindBySportCenterID(ctx, c.ID)
		if err != nil {
			return in, err
		}
		in.HasSchedules = len(rules) > 0
	}
	return in, nil
}

func (uc *CommercialUseCase) view(ctx context.Context, c *domain.SportCenter) (*CommercialView, error) {
	in, err := uc.publishInputs(ctx, c)
	if err != nil {
		return nil, err
	}
	check := domain.CanPublishSportCenter(c, in)
	mode := c.EffectiveCommercialMode()
	v := &CommercialView{
		CenterID:    c.ID.Hex(),
		PaymentMode: mode,
		Lifecycle:   check.Status,
		Published:   c.IsPublished(),
		Enforced:    CommercialModelEnforced(),
		Review:      c.Commercial != nil && c.Commercial.Review,
		Publish:     check,
	}
	switch mode {
	case domain.CommercialModePayAtCenter:
		sv := &SubscriptionView{Status: c.SubscriptionStatus()}
		if s := c.Commercial; s != nil && s.Subscription != nil {
			sv.Plan, sv.Amount = s.Subscription.Plan, s.Subscription.Amount
			sv.StartedAt, sv.NextChargeAt, sv.CancelledAt = s.Subscription.StartedAt, s.Subscription.NextChargeAt, s.Subscription.CancelledAt
		}
		v.Subscription = sv
	case domain.CommercialModePayOnline:
		v.MercadoPago = c.MercadoPagoStatus()
		rate, source := c.EffectiveCommission(CommercialModelEnforced(), defaultMarketplaceFeePercent)
		cv := &CommissionView{Effective: rate, Source: source}
		if c.Commercial != nil && c.Commercial.OnlinePayment != nil && c.Commercial.OnlinePayment.Commission != nil {
			cv.Configured = c.Commercial.OnlinePayment.Commission.Value
		}
		v.Commission = cv
	}
	return v, nil
}

// Get devuelve la configuración comercial del centro.
func (uc *CommercialUseCase) Get(ctx context.Context, centerID primitive.ObjectID) (*CommercialView, error) {
	c, err := uc.center(ctx, centerID)
	if err != nil {
		return nil, err
	}
	return uc.view(ctx, c)
}

// ValidatePublish informa qué falta para publicar.
func (uc *CommercialUseCase) ValidatePublish(ctx context.Context, centerID primitive.ObjectID) (domain.PublishCheck, error) {
	c, err := uc.center(ctx, centerID)
	if err != nil {
		return domain.PublishCheck{}, err
	}
	in, err := uc.publishInputs(ctx, c)
	if err != nil {
		return domain.PublishCheck{}, err
	}
	return domain.CanPublishSportCenter(c, in), nil
}

// Publish publica el centro si cumple todas las reglas. La validación vive
// aquí, nunca solo en el frontend.
func (uc *CommercialUseCase) Publish(ctx context.Context, centerID primitive.ObjectID) (*CommercialView, error) {
	c, err := uc.center(ctx, centerID)
	if err != nil {
		return nil, err
	}
	if c.Status == domain.SportCenterStatusSuspended || c.Status == domain.SportCenterStatusBlocked {
		return nil, domain.NewConflictError("el centro está suspendido")
	}
	in, err := uc.publishInputs(ctx, c)
	if err != nil {
		return nil, err
	}
	if check := domain.CanPublishSportCenter(c, in); !check.CanPublish {
		return nil, domain.NewBadRequestError("el centro no cumple los requisitos para publicarse: " + strings.Join(check.Missing, ", "))
	}
	now := time.Now().UTC()
	if err := uc.repo.UpdateCommercialFields(ctx, c.ID, map[string]interface{}{
		"commercial.published":    true,
		"commercial.published_at": now,
	}); err != nil {
		return nil, err
	}
	return uc.Get(ctx, c.ID)
}

// SetMode cambia la modalidad comercial. Nunca borra espacios, horarios,
// reservas ni comisiones históricas.
func (uc *CommercialUseCase) SetMode(ctx context.Context, centerID primitive.ObjectID, mode string) (*CommercialView, error) {
	var legacy string
	switch mode {
	case domain.CommercialModePayAtCenter:
		legacy = PaymentModePayAtCenter
	case domain.CommercialModePayOnline:
		legacy = PaymentModePayOnline
	default:
		return nil, domain.NewBadRequestError("modalidad inválida: usa PAY_AT_CENTER o PAY_ONLINE")
	}
	c, err := uc.center(ctx, centerID)
	if err != nil {
		return nil, err
	}
	if c.EffectiveCommercialMode() == mode {
		return uc.view(ctx, c)
	}

	if err := uc.modes.SetPaymentMode(ctx, c.ID, legacy); err != nil {
		return nil, err
	}

	fields := map[string]interface{}{"commercial.payment_mode": mode}
	switch mode {
	case domain.CommercialModePayOnline:
		if c.Commercial == nil || c.Commercial.OnlinePayment == nil || c.Commercial.OnlinePayment.Commission == nil {
			fields["commercial.online_payment.commission"] = domain.CommissionConfig{
				Type: domain.CommissionTypePercentage, Value: intRef(defaultMarketplaceFeePercent), Source: domain.CommissionSourceDefault,
			}
		}
		fields["commercial.online_payment.payment_provider"] = domain.PaymentProvider{
			Provider: domain.ProviderMercadoPago, Status: ProviderStatusFor(c),
		}
	case domain.CommercialModePayAtCenter:
		if c.Commercial == nil || c.Commercial.Subscription == nil {
			fields["commercial.subscription"] = domain.Subscription{Status: domain.SubscriptionNotConfigured}
		}
	}
	// Con el modelo activo, el centro publicado bajo otra modalidad debe volver
	// a cumplir los requisitos de la nueva antes de seguir publicado.
	if CommercialModelEnforced() && c.IsPublished() {
		candidate := *c
		candidate.Commercial = cloneCommercial(c.Commercial)
		candidate.Commercial.PaymentMode = mode
		in, err := uc.publishInputs(ctx, c)
		if err != nil {
			return nil, err
		}
		if !domain.CanPublishSportCenter(&candidate, in).CanPublish {
			fields["commercial.published"] = false
		}
	}
	if err := uc.repo.UpdateCommercialFields(ctx, c.ID, fields); err != nil {
		return nil, err
	}
	return uc.Get(ctx, c.ID)
}

// ProviderStatusFor es el estado de MP que se guarda en `commercial`.
func ProviderStatusFor(c *domain.SportCenter) string {
	return c.MercadoPagoStatus()
}

func cloneCommercial(c *domain.Commercial) *domain.Commercial {
	if c == nil {
		return &domain.Commercial{}
	}
	cp := *c
	return &cp
}

func intRef(v int) *int { return &v }

// SetCommission fija (o, con value nil, restablece al default) la comisión de
// un centro PAY_ONLINE. Solo superadmin. Las reservas históricas conservan la
// comisión con la que se cobraron.
func (uc *CommercialUseCase) SetCommission(ctx context.Context, centerID primitive.ObjectID, value *int) (*CommercialView, error) {
	c, err := uc.center(ctx, centerID)
	if err != nil {
		return nil, err
	}
	if c.EffectiveCommercialMode() != domain.CommercialModePayOnline {
		return nil, domain.NewBadRequestError("la comisión solo aplica a centros con pago anticipado")
	}
	cfg := domain.CommissionConfig{Type: domain.CommissionTypePercentage}
	if value == nil {
		cfg.Value, cfg.Source = intRef(defaultMarketplaceFeePercent), domain.CommissionSourceDefault
	} else {
		if *value < 0 || *value > 100 {
			return nil, domain.NewBadRequestError("la comisión debe estar entre 0 y 100")
		}
		cfg.Value, cfg.Source = value, domain.CommissionSourceCenter
	}
	if err := uc.repo.UpdateCommercialFields(ctx, c.ID, map[string]interface{}{
		"commercial.online_payment.commission": cfg,
	}); err != nil {
		return nil, err
	}
	return uc.Get(ctx, c.ID)
}

// SubscriptionStart es el resultado de iniciar la suscripción.
type SubscriptionStart struct {
	InitPoint string  `json:"init_point"`
	Amount    float64 `json:"amount"`
}

// StartSubscription crea el preapproval mensual en Mercado Pago. El estado
// pasa a ACTIVE solo cuando el webhook confirma la autorización.
func (uc *CommercialUseCase) StartSubscription(ctx context.Context, centerID primitive.ObjectID, payerEmail string) (*SubscriptionStart, error) {
	c, err := uc.center(ctx, centerID)
	if err != nil {
		return nil, err
	}
	if c.EffectiveCommercialMode() != domain.CommercialModePayAtCenter {
		return nil, domain.NewBadRequestError("la suscripción solo aplica a centros con modalidad solo reservas")
	}
	if c.SubscriptionStatus() == domain.SubscriptionActive {
		return nil, domain.NewConflictError("el centro ya tiene una suscripción activa")
	}
	if uc.provider == nil || uc.cfg.SubscriptionAmount <= 0 {
		return nil, domain.NewConflictError("el plan de suscripción no está configurado")
	}
	payerEmail = strings.TrimSpace(payerEmail)
	if payerEmail == "" {
		payerEmail = c.AdminEmail
	}
	if payerEmail == "" {
		return nil, domain.NewBadRequestError("falta el correo del pagador")
	}

	res, err := uc.provider.CreateSubscription(ctx, mercadopago.SubscriptionInput{
		Reason:      fmt.Sprintf("Suscripción ReservaloYA - %s", c.Name),
		ExternalRef: c.ID.Hex(),
		PayerEmail:  payerEmail,
		BackURL:     uc.cfg.SubscriptionBack,
		Amount:      uc.cfg.SubscriptionAmount,
	})
	if err != nil {
		return nil, err
	}
	if err := uc.repo.UpdateCommercialFields(ctx, c.ID, map[string]interface{}{
		"commercial.subscription": domain.Subscription{
			Status:      domain.SubscriptionPending,
			Plan:        domain.SubscriptionPlanMonthly,
			Amount:      uc.cfg.SubscriptionAmount,
			ProviderRef: res.ID,
		},
	}); err != nil {
		return nil, err
	}
	return &SubscriptionStart{InitPoint: res.InitPoint, Amount: uc.cfg.SubscriptionAmount}, nil
}

// ErrSubscriptionIgnored indica que el webhook no corresponde a la
// suscripción vigente del centro.
var ErrSubscriptionIgnored = errors.New("subscription webhook ignored")

func subscriptionStatusFromMP(status string) string {
	switch status {
	case "authorized":
		return domain.SubscriptionActive
	case "paused":
		return domain.SubscriptionPaused
	case "cancelled":
		return domain.SubscriptionCancelled
	default:
		return domain.SubscriptionPending
	}
}

// HandleSubscriptionWebhook sincroniza el estado de la suscripción. No confía
// en el cuerpo del webhook: vuelve a consultar el preapproval a Mercado Pago.
func (uc *CommercialUseCase) HandleSubscriptionWebhook(ctx context.Context, preapprovalID string) error {
	if uc.provider == nil || preapprovalID == "" {
		return ErrSubscriptionIgnored
	}
	res, err := uc.provider.GetSubscription(ctx, preapprovalID)
	if err != nil {
		return err
	}
	centerID, err := primitive.ObjectIDFromHex(res.ExternalRef)
	if err != nil {
		return ErrSubscriptionIgnored
	}
	c, err := uc.center(ctx, centerID)
	if err != nil {
		return ErrSubscriptionIgnored
	}
	if c.Commercial == nil || c.Commercial.Subscription == nil || c.Commercial.Subscription.ProviderRef != res.ID {
		logger.FromContext(ctx).Warnw("subscription_webhook_ref_mismatch", "center_id", centerID.Hex(), "preapproval_id", res.ID)
		return ErrSubscriptionIgnored
	}

	status := subscriptionStatusFromMP(res.Status)
	fields := map[string]interface{}{"commercial.subscription.status": status}
	now := time.Now().UTC()
	switch status {
	case domain.SubscriptionActive:
		if c.Commercial.Subscription.StartedAt == nil {
			fields["commercial.subscription.started_at"] = now
		}
		fields["commercial.subscription.next_charge_at"] = res.NextPayment
	case domain.SubscriptionCancelled:
		fields["commercial.subscription.cancelled_at"] = now
	}
	// Un centro publicado cuya suscripción deja de estar activa pierde la
	// publicación solo con el modelo comercial activo.
	if CommercialModelEnforced() && status != domain.SubscriptionActive && c.IsPublished() &&
		c.EffectiveCommercialMode() == domain.CommercialModePayAtCenter && c.Commercial.Subscription.Plan != domain.SubscriptionPlanLegacy {
		fields["commercial.published"] = false
	}
	return uc.repo.UpdateCommercialFields(ctx, c.ID, fields)
}
