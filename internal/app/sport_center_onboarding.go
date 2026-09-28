package app

// Wizard de onboarding post-login: el usuario completa la información de su
// centro deportivo por etapas, guardando cada una de forma independiente
// (RES-23..27). El centro creado en el registro (RES-22) arranca en estado
// "pending"; cada "Guardar y continuar" persiste la etapa y avanza
// onboarding_step; "Finalizar" marca onboarding_status = completed.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	OnboardingStatusPending   = "pending"
	OnboardingStatusCompleted = "completed"

	OnboardingStepContacto = 1 // teléfono de contacto
	OnboardingStepEspacios = 2 // tipos y cantidades de espacios
	OnboardingStepCobro    = 3 // modalidad de cobro
	OnboardingStepImagen   = 4 // imagen y oferta primer mes
	OnboardingStepFinal    = 5 // resumen / finalizar
)

// PaymentMode valores de modalidad comercial (RES-25).
const (
	PaymentModePayAtCenter = "pay_at_center" // solo reservas
	PaymentModePayOnline   = "pay_online"    // pago anticipado
)

// UpdateOnboardingInput es el payload de una etapa del wizard. Solo se aplican
// los campos correspondientes a la etapa indicada en Step.
type UpdateOnboardingInput struct {
	Step           int
	Nombre         *string
	Rut            *string
	Email          *string
	Direccion      *string
	Telefono       *string
	Templates     []domain.SpaceTemplate
	PaymentMode    *string
	ImageURL       *string
	FirstMonthFree *bool
	Finalize       bool
}

var chileanPhonePattern = regexp.MustCompile(`^\+56\d{8,10}$`)

// validarTelefonoChileno valida un teléfono chileno con código de país: +56
// seguido de 8 a 10 dígitos, tolerando espacios, guiones, puntos y paréntesis.
func validarTelefonoChileno(phone string) bool {
	normalized := phone
	for _, r := range []rune{' ', '-', '.', '(', ')'} {
		normalized = strings.ReplaceAll(normalized, string(r), "")
	}
	return chileanPhonePattern.MatchString(normalized)
}

func isValidPaymentMode(mode string) bool {
	return mode == PaymentModePayAtCenter || mode == PaymentModePayOnline
}

// applyPaymentModeToSlots fuerza las franjas (reglas semanales) al esquema de
// pago de la modalidad comercial del centro: con "pago anticipado" toda franja
// exige pago online; con "solo reservas" el pago se hace en el centro. Con
// modalidad vacía (centros legacy) se respeta lo enviado por el admin.
func applyPaymentModeToSlots(paymentMode string, slots []domain.ScheduleRuleSlot) []domain.ScheduleRuleSlot {
	switch paymentMode {
	case PaymentModePayOnline:
		out := make([]domain.ScheduleRuleSlot, len(slots))
		for i, s := range slots {
			out[i] = s
			out[i].PaymentRequired = true
			out[i].PaymentOptional = false
		}
		return out
	case PaymentModePayAtCenter:
		out := make([]domain.ScheduleRuleSlot, len(slots))
		for i, s := range slots {
			out[i] = s
			out[i].PaymentRequired = false
			out[i].PaymentOptional = false
		}
		return out
	default:
		return slots
	}
}

// applyPaymentModeToCourtSchedule es la versión legacy para spaces.schedule
// (slots por hora).
func applyPaymentModeToCourtSchedule(paymentMode string, schedule []domain.CourtSchedule) []domain.CourtSchedule {
	switch paymentMode {
	case PaymentModePayOnline:
		out := make([]domain.CourtSchedule, len(schedule))
		for i, s := range schedule {
			out[i] = s
			out[i].PaymentRequired = true
			out[i].PaymentOptional = false
		}
		return out
	case PaymentModePayAtCenter:
		out := make([]domain.CourtSchedule, len(schedule))
		for i, s := range schedule {
			out[i] = s
			out[i].PaymentRequired = false
			out[i].PaymentOptional = false
		}
		return out
	default:
		return schedule
	}
}

// UpdateSportCenterOnboarding persiste la etapa del wizard para el centro del
// usuario autenticado. Cada etapa valida sus propios campos y avanza
// onboarding_step; Finalize exige los datos mínimos y marca el centro completo.
func (uc *SportCenterUseCase) UpdateSportCenterOnboarding(ctx context.Context, userID string, input UpdateOnboardingInput) error {
	centers, err := uc.repo.FindByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("error al buscar tu centro: %v", err)
	}
	if len(centers) == 0 {
		return errors.New("no se encontró un centro deportivo asociado a tu cuenta")
	}
	center := centers[0]

	switch input.Step {
	case OnboardingStepContacto:
		// Datos del centro ya capturados en el registro (RES-22), editables
		// aquí. Solo se aplican los campos que llegan no vacíos.
		if v := strings.TrimSpace(valueOrEmpty(input.Nombre)); v != "" {
			if len(v) > 100 {
				return fmt.Errorf("el nombre no puede superar los 100 caracteres")
			}
			center.Name = v
		}
		if v := strings.TrimSpace(valueOrEmpty(input.Rut)); v != "" {
			if !validateRUT(v) {
				return fmt.Errorf("RUT inválido: %s", v)
			}
			center.Rut = v
		}
		if v := strings.TrimSpace(valueOrEmpty(input.Direccion)); v != "" {
			center.Address = v
		}
		if v := normalizeEmail(valueOrEmpty(input.Email)); v != "" {
			if !isValidEmail(v) {
				return fmt.Errorf("correo inválido: %s", v)
			}
			center.AdminEmail = v
			center.Contact.Email = v
		}

		phone := strings.TrimSpace(valueOrEmpty(input.Telefono))
		if !validarTelefonoChileno(phone) {
			return fmt.Errorf("teléfono inválido: usa formato chileno +56 9 XXXX XXXX")
		}
		center.Contact.Phone = phone
		center.Contact.Email = center.AdminEmail
	case OnboardingStepEspacios:
		// Se valida y persiste la configuración de espacios (tipo + cantidad +
		// horarios/valores) como borrador transitorio. Los espacios individuales
		// se materializan en la colección `spaces` al finalizar.
		templates, err := validarTemplates(input.Templates)
		if err != nil {
			return err
		}
		center.SpaceTemplates = templates
	case OnboardingStepCobro:
		mode := valueOrEmpty(input.PaymentMode)
		if !isValidPaymentMode(mode) {
			return fmt.Errorf("modalidad de cobro inválida")
		}
		center.PaymentMode = mode
	case OnboardingStepImagen:
		if input.ImageURL != nil {
			center.ImageURL = *input.ImageURL
		}
		if input.FirstMonthFree != nil {
			center.FirstMonthFree = *input.FirstMonthFree
		}
	default:
		// Step 5 (resumen) no persiste datos nuevos; se valida al finalizar.
	}

	if input.Finalize {
		if center.Contact.Phone == "" {
			return fmt.Errorf("te falta configurar el teléfono de contacto")
		}
		if center.PaymentMode == "" {
			return fmt.Errorf("te falta elegir la modalidad de cobro")
		}
		// Fuente de verdad: el borrador persistido en la etapa Espacios. Si no
		// hay borrador (p. ej. el frontend no guardó la etapa), se acepta el
		// payload como respaldo.
		templates := center.SpaceTemplates
		if len(templates) == 0 {
			templates = input.Templates
		}
		templates, err := validarTemplates(templates)
		if err != nil {
			return err
		}
		if center.OnboardingStatus != OnboardingStatusCompleted {
			if err := uc.materializeSpaces(ctx, center.ID, center.PaymentMode, templates); err != nil {
				return err
			}
		}
		// El borrador se consume al materializar: no debe quedar en el centro.
		center.SpaceTemplates = nil
		center.OnboardingStatus = OnboardingStatusCompleted
		center.OnboardingStep = OnboardingStepFinal
	} else if input.Step >= OnboardingStepContacto && input.Step <= OnboardingStepImagen {
		center.OnboardingStep = input.Step
	}

	center.UpdatedAt = time.Now()
	return uc.repo.Update(ctx, &center)
}

func valueOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// validarTemplates valida la configuración de espacios del wizard: tipos
// válidos, cantidad >= 1 (suma total >= 1) y reglas horarias válidas para
// canchas. Devuelve los templates normalizados.
func validarTemplates(input []domain.SpaceTemplate) ([]domain.SpaceTemplate, error) {
	seen := map[string]bool{}
	total := 0
	for i := range input {
		p := &input[i]
		p.Tipo = strings.TrimSpace(p.Tipo)
		if !domain.TipoEspacio(p.Tipo).IsValid() {
			return nil, fmt.Errorf("tipo de espacio inválido: %s", p.Tipo)
		}
		if seen[p.Tipo] {
			return nil, fmt.Errorf("tipo de espacio duplicado: %s", p.Tipo)
		}
		seen[p.Tipo] = true
		if p.Cantidad < 1 {
			return nil, fmt.Errorf("la cantidad de %s debe ser al menos 1", p.Tipo)
		}
		total += p.Cantidad

		p.Cancha = p.Tipo == "cancha"
		if p.Cancha {
			if err := validarSpaceTemplateRules(p.Rules); err != nil {
				return nil, fmt.Errorf("horarios de %s: %v", p.Tipo, err)
			}
		} else {
			if p.PricePerDay < 0 || p.PricePerMonth < 0 || p.PriceAllDay < 0 || p.PriceHalfDay < 0 {
				return nil, fmt.Errorf("valores de %s no pueden ser negativos", p.Tipo)
			}
		}
	}
	if total < 1 {
		return nil, fmt.Errorf("debes registrar al menos un espacio")
	}
	return input, nil
}

// validarSpaceTemplateRules valida las reglas semanales de la plantilla de cancha:
// días válidos (0..6), franjas ordenadas y sin solaparse, precio >= 0.
func validarSpaceTemplateRules(rules []domain.ScheduleRule) error {
	for _, r := range rules {
		if len(r.DaysOfWeek) == 0 {
			return fmt.Errorf("cada regla debe tener al menos un día")
		}
		daySeen := map[int]bool{}
		for _, d := range r.DaysOfWeek {
			if d < 0 || d > 6 {
				return fmt.Errorf("día de semana inválido: %d", d)
			}
			if daySeen[d] {
				return fmt.Errorf("día de semana duplicado en una regla")
			}
			daySeen[d] = true
		}
		if len(r.Slots) == 0 {
			return fmt.Errorf("cada regla debe tener al menos una franja")
		}
		prevEnd := 0
		for _, s := range r.Slots {
			start, err1 := timeToMinutes(s.StartTime)
			end, err2 := timeToMinutes(s.EndTime)
			if err1 != nil || err2 != nil || end <= start {
				return fmt.Errorf("franja inválida: %s-%s", s.StartTime, s.EndTime)
			}
			if start < prevEnd {
				return fmt.Errorf("franjas solapadas: %s-%s", s.StartTime, s.EndTime)
			}
			prevEnd = end
			if s.Price < 0 {
				return fmt.Errorf("el precio no puede ser negativo")
			}
		}
	}
	return nil
}

// spaceLabel devuelve la etiqueta singular para el nombre de un espacio.
func spaceLabel(tipo string) string {
	switch tipo {
	case "cancha":
		return "Cancha"
	case "quincho":
		return "Quincho"
	case "sede":
		return "Sede"
	case "piscina":
		return "Piscina"
	default:
		return "Espacio"
	}
}

// materializeSpaces crea los espacios individuales (colección `spaces`) y sus
// reglas de horario (`schedule_rules` para canchas) a partir de la
// configuración definida en el wizard de onboarding. Se invoca al finalizar.
// Las franjas de cancha heredan el esquema de pago de la modalidad comercial
// del centro (payment_mode), por lo que no son editables por horario.
func (uc *SportCenterUseCase) materializeSpaces(ctx context.Context, centerID primitive.ObjectID, paymentMode string, templates []domain.SpaceTemplate) error {
	for _, p := range templates {
		category := domain.SpaceCategory(p.Tipo)
		if !category.IsValid() || p.Cantidad < 1 {
			continue
		}
		for i := 1; i <= p.Cantidad; i++ {
			space := &domain.Space{
				SportCenterID: centerID,
				Category:      category,
				Name:          fmt.Sprintf("%s %d", spaceLabel(p.Tipo), i),
				Enabled:       true,
			}
			if category.IsFacility() {
				space.Type = domain.FacilityType(category)
				switch category {
				case domain.SpaceCategoryQuincho:
					space.PriceAllDay = p.PriceAllDay
					space.PriceHalfDay = p.PriceHalfDay
				default: // sede / piscina
					space.PricePerDay = p.PricePerDay
					space.PricePerMonth = p.PricePerMonth
				}
			}
			if err := uc.spaceRepo.Create(ctx, space); err != nil {
				return fmt.Errorf("error al crear el espacio %s %d: %v", p.Tipo, i, err)
			}
			if category == domain.SpaceCategoryCancha {
				for _, rule := range p.Rules {
					r := rule
					r.SpaceID = space.ID
					r.SportCenterID = centerID
					r.Active = true
					r.Slots = applyPaymentModeToSlots(paymentMode, r.Slots)
					if err := uc.ruleRepo.Create(ctx, &r); err != nil {
						return fmt.Errorf("error al crear la regla de horario del espacio %s %d: %v", p.Tipo, i, err)
					}
				}
			}
		}
	}
	return nil
}