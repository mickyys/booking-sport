package app

// Estado del onboarding calculado con datos reales (espacios, horarios,
// contacto, modalidad de cobro). Lo consumen la web y la app mobile para
// mostrar el mismo avance sin depender de cómo se creó cada cosa: el wizard web
// guarda plantillas y la app crea los espacios directamente.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Claves de los pasos del onboarding.
const (
	OnboardingKeyContact     = "contact"
	OnboardingKeySpaces      = "spaces"
	OnboardingKeySchedules   = "schedules"
	OnboardingKeyPaymentMode = "payment_mode"
	OnboardingKeyBilling     = "billing"
	OnboardingKeyProfile     = "profile"
)

// OnboardingStepStatus es el estado de un paso del onboarding.
type OnboardingStepStatus struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Done     bool   `json:"done"`
	Optional bool   `json:"optional"`
	Detail   string `json:"detail,omitempty"`
}

// OnboardingStatus es el avance del onboarding de un centro.
type OnboardingStatus struct {
	Status      string                 `json:"onboarding_status"`
	Step        int                    `json:"onboarding_step"`
	Steps       []OnboardingStepStatus `json:"steps"`
	CanComplete bool                   `json:"can_complete"`
	Missing     []string               `json:"missing"`
	PaymentMode string                 `json:"payment_mode"`
	// SpacesCount cuenta solo espacios reales (colección spaces).
	SpacesCount int `json:"spaces_count"`
	// DraftSpaces es la cantidad de espacios del borrador del wizard web que
	// aún no se materializan.
	DraftSpaces          int    `json:"draft_spaces"`
	CourtsCount          int    `json:"courts_count"`
	CourtsWithSchedule   int    `json:"courts_with_schedule"`
	SubscriptionStatus   string `json:"subscription_status,omitempty"`
	MercadoPagoConnected bool   `json:"mercadopago_connected"`
}

// OnboardingIncompleteError indica que faltan requisitos para completar el
// onboarding con los datos existentes.
type OnboardingIncompleteError struct {
	Missing []string
}

func (e *OnboardingIncompleteError) Error() string {
	return "faltan datos para completar el onboarding: " + strings.Join(e.Missing, ", ")
}

func (uc *SportCenterUseCase) centerByUser(ctx context.Context, userID string) (*domain.SportCenter, error) {
	centers, err := uc.repo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("error al buscar tu centro: %v", err)
	}
	if len(centers) == 0 {
		return nil, errors.New("no se encontró un centro deportivo asociado a tu cuenta")
	}
	return &centers[0], nil
}

// onboardingStatusFor calcula el avance a partir de los datos reales.
func (uc *SportCenterUseCase) onboardingStatusFor(ctx context.Context, c *domain.SportCenter) (*OnboardingStatus, error) {
	st := &OnboardingStatus{
		Status:               c.OnboardingStatus,
		Step:                 c.OnboardingStep,
		PaymentMode:          c.PaymentMode,
		SubscriptionStatus:   c.SubscriptionStatus(),
		MercadoPagoConnected: c.MercadoPago.IsConnected(),
		Missing:              []string{},
	}
	for _, t := range c.SpaceTemplates {
		st.DraftSpaces += t.Cantidad
	}

	rulesBySpace := map[primitive.ObjectID]bool{}
	if uc.spaceRepo != nil {
		spaces, err := uc.spaceRepo.FindByCenterID(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		if uc.ruleRepo != nil {
			rules, err := uc.ruleRepo.FindBySportCenterID(ctx, c.ID)
			if err != nil {
				return nil, err
			}
			for _, r := range rules {
				rulesBySpace[r.SpaceID] = true
			}
		}
		for _, s := range spaces {
			if !s.Enabled {
				continue
			}
			st.SpacesCount++
			if s.Category == domain.SpaceCategoryCancha {
				st.CourtsCount++
				if rulesBySpace[s.ID] || len(s.Schedule) > 0 {
					st.CourtsWithSchedule++
				}
			}
		}
	}

	contactDone := strings.TrimSpace(c.Contact.Phone) != ""
	spacesDone := st.SpacesCount > 0
	// Sin canchas no hay horarios que configurar (quinchos, sedes y piscinas se
	// reservan por día o mes).
	schedulesDone := spacesDone && st.CourtsWithSchedule >= st.CourtsCount
	paymentDone := isValidPaymentMode(c.PaymentMode)

	billingDone := false
	billingDetail := "Elige primero la modalidad de cobro"
	switch c.PaymentMode {
	case PaymentModePayOnline:
		billingDone = st.MercadoPagoConnected
		billingDetail = "Conecta tu cuenta de Mercado Pago"
		if billingDone {
			billingDetail = "Mercado Pago conectado"
		}
	case PaymentModePayAtCenter:
		billingDone = st.SubscriptionStatus == domain.SubscriptionActive
		billingDetail = "Activa la suscripción mensual"
		if billingDone {
			billingDetail = "Suscripción activa"
		}
	}

	profileDone := c.ImageURL != ""

	schedulesDetail := "Agrega al menos un espacio"
	if spacesDone {
		schedulesDetail = fmt.Sprintf("%d de %d canchas con horario", st.CourtsWithSchedule, st.CourtsCount)
		if st.CourtsCount == 0 {
			schedulesDetail = "No necesitas horarios para estos espacios"
		}
	}

	paymentDetail := ""
	switch c.PaymentMode {
	case PaymentModePayAtCenter:
		paymentDetail = "Solo reservas"
	case PaymentModePayOnline:
		paymentDetail = "Pago anticipado"
	}

	st.Steps = []OnboardingStepStatus{
		{Key: OnboardingKeyContact, Label: "Contacto", Done: contactDone, Detail: strings.TrimSpace(c.Contact.Phone)},
		{Key: OnboardingKeySpaces, Label: "Espacios", Done: spacesDone, Detail: fmt.Sprintf("%d espacio(s)", st.SpacesCount)},
		{Key: OnboardingKeySchedules, Label: "Horarios", Done: schedulesDone, Detail: schedulesDetail},
		{Key: OnboardingKeyPaymentMode, Label: "Modalidad de cobro", Done: paymentDone, Detail: paymentDetail},
		{Key: OnboardingKeyBilling, Label: "Suscripción o Mercado Pago", Done: billingDone, Optional: true, Detail: billingDetail},
		{Key: OnboardingKeyProfile, Label: "Imagen del centro", Done: profileDone, Optional: true},
	}
	for _, s := range st.Steps {
		if !s.Done && !s.Optional {
			st.Missing = append(st.Missing, s.Key)
		}
	}
	st.CanComplete = len(st.Missing) == 0
	return st, nil
}

// GetOnboardingStatus devuelve el avance del onboarding del centro del usuario.
func (uc *SportCenterUseCase) GetOnboardingStatus(ctx context.Context, userID string) (*OnboardingStatus, error) {
	c, err := uc.centerByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return uc.onboardingStatusFor(ctx, c)
}

// CompleteOnboardingFromExisting cierra el onboarding usando los espacios y
// horarios ya creados (p. ej. desde la app mobile). No materializa plantillas,
// así que no duplica espacios. Es idempotente.
func (uc *SportCenterUseCase) CompleteOnboardingFromExisting(ctx context.Context, userID string) (*OnboardingStatus, error) {
	c, err := uc.centerByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	st, err := uc.onboardingStatusFor(ctx, c)
	if err != nil {
		return nil, err
	}
	if c.OnboardingStatus == OnboardingStatusCompleted {
		return st, nil
	}
	if !st.CanComplete {
		return st, &OnboardingIncompleteError{Missing: st.Missing}
	}
	if err := uc.markOnboardingCompleted(ctx, c); err != nil {
		return nil, err
	}
	st.Status = OnboardingStatusCompleted
	st.Step = OnboardingStepFinal
	return st, nil
}

// ForceCompleteOnboarding lo usa el superadmin para cerrar el onboarding de un
// centro aunque falten datos. La auditoría la registra el handler.
func (uc *SportCenterUseCase) ForceCompleteOnboarding(ctx context.Context, centerID primitive.ObjectID) error {
	c, err := uc.repo.FindByID(ctx, centerID)
	if err != nil || c == nil {
		return errors.New("no se encontró el centro deportivo")
	}
	return uc.markOnboardingCompleted(ctx, c)
}

func (uc *SportCenterUseCase) markOnboardingCompleted(ctx context.Context, c *domain.SportCenter) error {
	c.OnboardingStatus = OnboardingStatusCompleted
	c.OnboardingStep = OnboardingStepFinal
	// El borrador del wizard ya no aplica: los espacios reales son la fuente de
	// verdad y materializarlo ahora los duplicaría.
	c.SpaceTemplates = nil
	c.UpdatedAt = time.Now()
	return uc.repo.Update(ctx, c)
}
