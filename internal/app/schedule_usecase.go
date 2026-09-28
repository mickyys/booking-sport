package app

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ScheduleRuleRepository es el contrato de persistencia de reglas semanales.
type ScheduleRuleRepository interface {
	Create(ctx context.Context, rule *domain.ScheduleRule) error
	Update(ctx context.Context, rule *domain.ScheduleRule) error
	FindByID(ctx context.Context, id primitive.ObjectID) (*domain.ScheduleRule, error)
	FindBySpaceID(ctx context.Context, spaceID primitive.ObjectID) ([]domain.ScheduleRule, error)
	FindBySportCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.ScheduleRule, error)
	Delete(ctx context.Context, id primitive.ObjectID) error
	DeleteBySpaceID(ctx context.Context, spaceID primitive.ObjectID) error
}

// ScheduleExceptionRepository es el contrato de persistencia de excepciones.
type ScheduleExceptionRepository interface {
	Create(ctx context.Context, ex *domain.ScheduleException) error
	Update(ctx context.Context, ex *domain.ScheduleException) error
	FindByID(ctx context.Context, id primitive.ObjectID) (*domain.ScheduleException, error)
	FindBySpaceID(ctx context.Context, spaceID primitive.ObjectID) ([]domain.ScheduleException, error)
	FindBySportCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.ScheduleException, error)
	FindBySpaceIDAndDate(ctx context.Context, spaceID primitive.ObjectID, date time.Time) (*domain.ScheduleException, error)
	Delete(ctx context.Context, id primitive.ObjectID) error
}

// BlockRepository es el contrato de persistencia de bloqueos.
type BlockRepository interface {
	Create(ctx context.Context, block *domain.Block) error
	FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Block, error)
	FindBySpaceID(ctx context.Context, spaceID primitive.ObjectID) ([]domain.Block, error)
	FindBySportCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.Block, error)
	FindBySpaceIDAndDate(ctx context.Context, spaceID primitive.ObjectID, date time.Time) ([]domain.Block, error)
	Delete(ctx context.Context, id primitive.ObjectID) error
}

type ScheduleUseCase struct {
	ruleRepo      ScheduleRuleRepository
	exceptionRepo ScheduleExceptionRepository
	blockRepo     BlockRepository
	spaceRepo     SpaceRepository
	centerRepo    SportCenterRepository
}

func NewScheduleUseCase(
	ruleRepo ScheduleRuleRepository,
	exceptionRepo ScheduleExceptionRepository,
	blockRepo BlockRepository,
	spaceRepo SpaceRepository,
	centerRepo SportCenterRepository,
) *ScheduleUseCase {
	return &ScheduleUseCase{
		ruleRepo:      ruleRepo,
		exceptionRepo: exceptionRepo,
		blockRepo:     blockRepo,
		spaceRepo:     spaceRepo,
		centerRepo:    centerRepo,
	}
}

// authorizeSpace verifica que el usuario administre el centro del espacio y
// retorna el espacio y el centro.
func (uc *ScheduleUseCase) authorizeSpace(ctx context.Context, spaceID primitive.ObjectID, userID string) (*domain.Space, *domain.SportCenter, error) {
	space, err := uc.spaceRepo.FindByID(ctx, spaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("space not found: %w", err)
	}
	center, err := uc.centerRepo.FindByID(ctx, space.SportCenterID)
	if err != nil {
		return nil, nil, fmt.Errorf("sport center not found: %w", err)
	}
	if !centerOwnsUser(center, userID) {
		return nil, nil, fmt.Errorf("user is not authorized for this sport center")
	}
	return space, center, nil
}

// GetSchedule retorna las reglas semanales de un espacio.
func (uc *ScheduleUseCase) GetSchedule(ctx context.Context, spaceID primitive.ObjectID, userID string) ([]domain.ScheduleRule, error) {
	if _, _, err := uc.authorizeSpace(ctx, spaceID, userID); err != nil {
		return nil, err
	}
	return uc.ruleRepo.FindBySpaceID(ctx, spaceID)
}

// SaveSchedule reemplaza las reglas semanales de un espacio (semántica PUT).
func (uc *ScheduleUseCase) SaveSchedule(ctx context.Context, spaceID primitive.ObjectID, userID string, rules []domain.ScheduleRule) ([]domain.ScheduleRule, error) {
	space, center, err := uc.authorizeSpace(ctx, spaceID, userID)
	if err != nil {
		return nil, err
	}

	rules = normalizeRuleSet(rules)
	if err := validateRuleSet(rules); err != nil {
		return nil, err
	}

	if err := uc.ruleRepo.DeleteBySpaceID(ctx, spaceID); err != nil {
		return nil, fmt.Errorf("error replacing rules: %w", err)
	}

	saved := make([]domain.ScheduleRule, 0, len(rules))
	for i := range rules {
		rule := rules[i]
		rule.ID = primitive.NewObjectID()
		rule.SpaceID = space.ID
		rule.SportCenterID = center.ID
		rule.Active = true
		// El esquema de pago de las franjas de cancha está determinado por la
		// modalidad comercial del centro (tipo de suscripción); no es editable.
		if space.Category == domain.SpaceCategoryCancha {
			rule.Slots = applyPaymentModeToSlots(center.PaymentMode, rule.Slots)
		}
		if err := uc.ruleRepo.Create(ctx, &rule); err != nil {
			return nil, fmt.Errorf("error saving rule: %w", err)
		}
		saved = append(saved, rule)
	}
	return saved, nil
}

// ListExceptions retorna las excepciones de un espacio.
func (uc *ScheduleUseCase) ListExceptions(ctx context.Context, spaceID primitive.ObjectID, userID string) ([]domain.ScheduleException, error) {
	if _, _, err := uc.authorizeSpace(ctx, spaceID, userID); err != nil {
		return nil, err
	}
	return uc.exceptionRepo.FindBySpaceID(ctx, spaceID)
}

// CreateException crea una excepción de horario para un espacio.
func (uc *ScheduleUseCase) CreateException(ctx context.Context, spaceID primitive.ObjectID, userID string, ex *domain.ScheduleException) (*domain.ScheduleException, error) {
	space, center, err := uc.authorizeSpace(ctx, spaceID, userID)
	if err != nil {
		return nil, err
	}

	if err := validateException(ex); err != nil {
		return nil, err
	}

	ex.SpaceID = space.ID
	ex.SportCenterID = center.ID
	ex.Date = normalizeDate(ex.Date)
	ex.ID = primitive.NewObjectID()
	if space.Category == domain.SpaceCategoryCancha {
		ex.Slots = applyPaymentModeToSlots(center.PaymentMode, ex.Slots)
	}
	if err := uc.exceptionRepo.Create(ctx, ex); err != nil {
		return nil, err
	}
	return ex, nil
}

// UpdateException actualiza una excepción de horario.
func (uc *ScheduleUseCase) UpdateException(ctx context.Context, spaceID primitive.ObjectID, exID primitive.ObjectID, userID string, updated *domain.ScheduleException) (*domain.ScheduleException, error) {
	space, center, err := uc.authorizeSpace(ctx, spaceID, userID)
	if err != nil {
		return nil, err
	}

	existing, err := uc.exceptionRepo.FindByID(ctx, exID)
	if err != nil {
		return nil, fmt.Errorf("exception not found: %w", err)
	}
	if existing.SpaceID != spaceID {
		return nil, fmt.Errorf("exception does not belong to this space")
	}

	if err := validateException(updated); err != nil {
		return nil, err
	}

	existing.Type = updated.Type
	existing.Date = normalizeDate(updated.Date)
	if space.Category == domain.SpaceCategoryCancha {
		existing.Slots = applyPaymentModeToSlots(center.PaymentMode, updated.Slots)
	} else {
		existing.Slots = updated.Slots
	}
	existing.Note = updated.Note
	if err := uc.exceptionRepo.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// DeleteException elimina una excepción de horario.
func (uc *ScheduleUseCase) DeleteException(ctx context.Context, spaceID primitive.ObjectID, exID primitive.ObjectID, userID string) error {
	if _, _, err := uc.authorizeSpace(ctx, spaceID, userID); err != nil {
		return err
	}
	existing, err := uc.exceptionRepo.FindByID(ctx, exID)
	if err != nil {
		return fmt.Errorf("exception not found: %w", err)
	}
	if existing.SpaceID != spaceID {
		return fmt.Errorf("exception does not belong to this space")
	}
	return uc.exceptionRepo.Delete(ctx, exID)
}

// ListBlocks retorna los bloqueos de un espacio.
func (uc *ScheduleUseCase) ListBlocks(ctx context.Context, spaceID primitive.ObjectID, userID string) ([]domain.Block, error) {
	if _, _, err := uc.authorizeSpace(ctx, spaceID, userID); err != nil {
		return nil, err
	}
	return uc.blockRepo.FindBySpaceID(ctx, spaceID)
}

// CreateBlock crea un bloqueo puntual de agenda.
func (uc *ScheduleUseCase) CreateBlock(ctx context.Context, spaceID primitive.ObjectID, userID string, block *domain.Block) (*domain.Block, error) {
	space, center, err := uc.authorizeSpace(ctx, spaceID, userID)
	if err != nil {
		return nil, err
	}

	if err := validateBlock(block); err != nil {
		return nil, err
	}

	block.SpaceID = space.ID
	block.SportCenterID = center.ID
	block.Date = normalizeDate(block.Date)
	block.ID = primitive.NewObjectID()
	if err := uc.blockRepo.Create(ctx, block); err != nil {
		return nil, err
	}
	return block, nil
}

// DeleteBlock elimina un bloqueo puntual.
func (uc *ScheduleUseCase) DeleteBlock(ctx context.Context, spaceID primitive.ObjectID, blockID primitive.ObjectID, userID string) error {
	if _, _, err := uc.authorizeSpace(ctx, spaceID, userID); err != nil {
		return err
	}
	existing, err := uc.blockRepo.FindByID(ctx, blockID)
	if err != nil {
		return fmt.Errorf("block not found: %w", err)
	}
	if existing.SpaceID != spaceID {
		return fmt.Errorf("block does not belong to this space")
	}
	return uc.blockRepo.Delete(ctx, blockID)
}

// SpaceAvailabilityResponse es la disponibilidad pública de un espacio para
// una fecha (sin autenticación), útil para facilities (quincho/sede/piscina).
type SpaceAvailabilityResponse struct {
	SpaceID  string                      `json:"space_id"`
	Date     string                      `json:"date"`
	HasRules bool                        `json:"has_rules"`
	Closed   bool                        `json:"closed"`
	Blocked  bool                        `json:"blocked"`
	Franjas  []domain.ScheduleRuleSlot   `json:"franjas"`
}

// PublicAvailability resuelve la disponibilidad de un espacio para una fecha
// sin requerir autorización. Retorna las franjas efectivas (luego de
// excepciones) y si la fecha está cerrada o bloqueada.
func (uc *ScheduleUseCase) PublicAvailability(ctx context.Context, spaceID primitive.ObjectID, date time.Time) (*SpaceAvailabilityResponse, error) {
	if _, err := uc.spaceRepo.FindByID(ctx, spaceID); err != nil {
		return nil, fmt.Errorf("space not found: %w", err)
	}

	resolver := NewScheduleResolver(uc.ruleRepo, uc.exceptionRepo, uc.blockRepo)
	resolved, err := resolver.ResolveFranjasForDate(ctx, spaceID, date)
	if err != nil {
		return nil, err
	}

	resp := &SpaceAvailabilityResponse{
		SpaceID:  spaceID.Hex(),
		Date:     date.Format("2006-01-02"),
		HasRules: resolved.HasRules,
		Franjas:  resolved.Franjas,
	}
	if resolved.Exception != nil && resolved.Exception.Type == domain.ScheduleExceptionClosed {
		resp.Closed = true
	}
	if len(resolved.Blocks) > 0 {
		resp.Blocked = true
	}
	return resp, nil
}

// PublicAvailabilityRange resuelve la disponibilidad pública de un espacio para
// un rango de fechas. Retorna un mapa fecha ("YYYY-MM-DD") → disponibilidad.
func (uc *ScheduleUseCase) PublicAvailabilityRange(ctx context.Context, spaceID primitive.ObjectID, from, to time.Time) (map[string]*SpaceAvailabilityResponse, error) {
	loc, _ := time.LoadLocation("America/Santiago")
	start := time.Date(from.In(loc).Year(), from.In(loc).Month(), from.In(loc).Day(), 0, 0, 0, 0, loc)
	end := time.Date(to.In(loc).Year(), to.In(loc).Month(), to.In(loc).Day(), 0, 0, 0, 0, loc)
	if end.Before(start) {
		return nil, fmt.Errorf("invalid range: from is after to")
	}
	const maxDays = 62
	if days := int(end.Sub(start).Hours()/24) + 1; days > maxDays {
		return nil, fmt.Errorf("range too large, max %d days", maxDays)
	}

	out := make(map[string]*SpaceAvailabilityResponse)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		resp, err := uc.PublicAvailability(ctx, spaceID, d)
		if err != nil {
			return nil, err
		}
		out[d.Format("2006-01-02")] = resp
	}
	return out, nil
}

// ==================== Helpers de validación ====================

// timeToMinutes convierte "HH:MM" a minutos desde las 00:00.
func timeToMinutes(t string) (int, error) {
	parts := strings.Split(t, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid time %q, expected HH:MM", t)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("invalid time %q, expected HH:MM", t)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid time %q, expected HH:MM", t)
	}
	// "24:00" es el centinela de fin de día (medianoche).
	if h == 24 && m == 0 {
		return 1440, nil
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("invalid time %q, expected HH:MM", t)
	}
	return h*60 + m, nil
}

// normalizeDate lleva una fecha a la medianoche de Chile (igual que bookings).
func normalizeDate(t time.Time) time.Time {
	loc, _ := time.LoadLocation("America/Santiago")
	return time.Date(t.In(loc).Year(), t.In(loc).Month(), t.In(loc).Day(), 0, 0, 0, 0, loc)
}

// interval es un rango [inicio, fin) en minutos.
type interval struct {
	start int
	end   int
}

func intervalsOverlap(a, b interval) bool {
	return a.start < b.end && b.start < a.end
}

func validateSlotList(slots []domain.ScheduleRuleSlot) error {
	for _, s := range slots {
		start, err := timeToMinutes(s.StartTime)
		if err != nil {
			return err
		}
		end, err := timeToMinutes(s.EndTime)
		if err != nil {
			return err
		}
		if end <= start {
			return fmt.Errorf("end time %s must be after start time %s", s.EndTime, s.StartTime)
		}
		if s.Price < 0 {
			return fmt.Errorf("price cannot be negative")
		}
	}

	for i := 0; i < len(slots); i++ {
		si, _ := timeToMinutes(slots[i].StartTime)
		ei, _ := timeToMinutes(slots[i].EndTime)
		for j := i + 1; j < len(slots); j++ {
			sj, _ := timeToMinutes(slots[j].StartTime)
			ej, _ := timeToMinutes(slots[j].EndTime)
			if intervalsOverlap(interval{si, ei}, interval{sj, ej}) {
				return fmt.Errorf("slots %s-%s and %s-%s overlap", slots[i].StartTime, slots[i].EndTime, slots[j].StartTime, slots[j].EndTime)
			}
		}
	}
	return nil
}

func normalizeDays(days []int) ([]int, error) {
	seen := make(map[int]bool)
	out := make([]int, 0, len(days))
	for _, d := range days {
		if d < 0 || d > 6 {
			return nil, fmt.Errorf("invalid day_of_week %d, expected 0..6", d)
		}
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("days_of_week cannot be empty")
	}
	sort.Ints(out)
	return out, nil
}

// normalizeRuleSet asegura que cada día de la semana pertenezca a una sola
// regla. Si un día aparece en varias, se conserva en la regla más específica
// (la de menos días; empate → la primera), replicando la prioridad de
// mostSpecificRuleForDay. Retorna las reglas resultantes, descartando las que
// quedan sin días.
func normalizeRuleSet(rules []domain.ScheduleRule) []domain.ScheduleRule {
	if len(rules) < 2 {
		return rules
	}
	spec := make([]int, len(rules))
	for i := range rules {
		spec[i] = len(rules[i].DaysOfWeek)
	}
	for d := 0; d < 7; d++ {
		owners := make([]int, 0)
		for i := range rules {
			if containsDay(rules[i].DaysOfWeek, d) {
				owners = append(owners, i)
			}
		}
		if len(owners) <= 1 {
			continue
		}
		sort.SliceStable(owners, func(a, b int) bool {
			return spec[owners[a]] < spec[owners[b]]
		})
		for _, i := range owners[1:] {
			days := rules[i].DaysOfWeek[:0]
			for _, x := range rules[i].DaysOfWeek {
				if x != d {
					days = append(days, x)
				}
			}
			rules[i].DaysOfWeek = days
		}
	}
	out := rules[:0]
	for i := range rules {
		if len(rules[i].DaysOfWeek) > 0 {
			out = append(out, rules[i])
		}
	}
	return out
}

// validateRuleSet valida un conjunto de reglas y que no generen doble
// disponibilidad para el mismo día.
func validateRuleSet(rules []domain.ScheduleRule) error {
	if len(rules) == 0 {
		return nil
	}

	// intervalos por día para detectar solapes entre reglas.
	perDay := make(map[int][]interval)

	for i := range rules {
		days, err := normalizeDays(rules[i].DaysOfWeek)
		if err != nil {
			return err
		}
		rules[i].DaysOfWeek = days
		if err := validateSlotList(rules[i].Slots); err != nil {
			return fmt.Errorf("rule %d: %w", i+1, err)
		}
		for _, s := range rules[i].Slots {
			start, _ := timeToMinutes(s.StartTime)
			end, _ := timeToMinutes(s.EndTime)
			iv := interval{start, end}
			for _, d := range days {
				for _, existing := range perDay[d] {
					if intervalsOverlap(existing, iv) {
						return fmt.Errorf("rules overlap on day %d: %d-%d conflicts with %d-%d", d, iv.start, iv.end, existing.start, existing.end)
					}
				}
				perDay[d] = append(perDay[d], iv)
			}
		}
	}
	return nil
}

func validateException(ex *domain.ScheduleException) error {
	switch ex.Type {
	case domain.ScheduleExceptionClosed:
		if ex.Date.IsZero() {
			return fmt.Errorf("date is required")
		}
	case domain.ScheduleExceptionSpecial:
		if ex.Date.IsZero() {
			return fmt.Errorf("date is required")
		}
		if len(ex.Slots) == 0 {
			return fmt.Errorf("special exception requires at least one slot")
		}
		if err := validateSlotList(ex.Slots); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid exception type %q, expected closed or special", ex.Type)
	}
	return nil
}

func validateBlock(block *domain.Block) error {
	if block.Date.IsZero() {
		return fmt.Errorf("date is required")
	}
	start, err := timeToMinutes(block.StartTime)
	if err != nil {
		return err
	}
	end, err := timeToMinutes(block.EndTime)
	if err != nil {
		return err
	}
	if end <= start {
		return fmt.Errorf("end time %s must be after start time %s", block.EndTime, block.StartTime)
	}
	return nil
}
