package app

import (
	"context"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ScheduleResolver resuelve la disponibilidad de un espacio para una fecha
// usando el modelo de reglas con prioridad:
//
//	Booking → Block → Exception → Rule → disponible
//
// Si el espacio aún no tiene reglas, HasRules=false y el caller debe usar el
// modelo legacy (spaces.schedule).
type ScheduleResolver struct {
	ruleRepo      ScheduleRuleRepository
	exceptionRepo ScheduleExceptionRepository
	blockRepo     BlockRepository
}

func NewScheduleResolver(ruleRepo ScheduleRuleRepository, exceptionRepo ScheduleExceptionRepository, blockRepo BlockRepository) *ScheduleResolver {
	return &ScheduleResolver{
		ruleRepo:      ruleRepo,
		exceptionRepo: exceptionRepo,
		blockRepo:     blockRepo,
	}
}

// ResolvedSchedule es el resultado de resolver la disponibilidad de un espacio
// en una fecha: las franjas efectivas, los bloqueos y la excepción aplicada.
type ResolvedSchedule struct {
	HasRules  bool
	Franjas   []domain.ScheduleRuleSlot
	Blocks    []domain.Block
	Exception *domain.ScheduleException
}

// ResolveFranjasForDate retorna las franjas efectivas para el espacio en `date`.
// La excepción (closed|special) tiene prioridad sobre las reglas semanales.
func (r *ScheduleResolver) ResolveFranjasForDate(ctx context.Context, spaceID primitive.ObjectID, date time.Time) (*ResolvedSchedule, error) {
	rules, err := r.ruleRepo.FindBySpaceID(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return &ResolvedSchedule{HasRules: false}, nil
	}

	loc, _ := time.LoadLocation("America/Santiago")
	dateCL := date.In(loc)
	searchDate := time.Date(dateCL.Year(), dateCL.Month(), dateCL.Day(), 0, 0, 0, 0, loc)
	weekday := int(searchDate.Weekday())

	res := &ResolvedSchedule{HasRules: true}

	ex, err := r.exceptionRepo.FindBySpaceIDAndDate(ctx, spaceID, searchDate)
	if err != nil {
		return nil, err
	}
	if ex != nil {
		res.Exception = ex
		if ex.Type == domain.ScheduleExceptionSpecial {
			res.Franjas = ex.Slots
		} else {
			// closed: sin disponibilidad
			res.Franjas = nil
		}
	} else {
		rule := mostSpecificRuleForDay(rules, weekday)
		if rule == nil {
			res.Franjas = nil
		} else {
			res.Franjas = rule.Slots
		}
	}

	blocks, err := r.blockRepo.FindBySpaceIDAndDate(ctx, spaceID, searchDate)
	if err != nil {
		return nil, err
	}
	res.Blocks = blocks
	return res, nil
}

// FindFranjaForBooking retorna la franja que contiene el intervalo
// [start, start+duration] de una reserva para una fecha. Si el espacio no
// tiene reglas, retorna (nil, false, nil) para usar el modelo legacy.
// Si tiene reglas pero ninguna franja cubre el intervalo, retorna (nil, true, nil).
func (r *ScheduleResolver) FindFranjaForBooking(ctx context.Context, spaceID primitive.ObjectID, date time.Time, hour, minutes, durationMinutes int) (*domain.ScheduleRuleSlot, bool, error) {
	resolved, err := r.ResolveFranjasForDate(ctx, spaceID, date)
	if err != nil {
		return nil, false, err
	}
	if !resolved.HasRules {
		return nil, false, nil
	}
	if durationMinutes <= 0 {
		durationMinutes = 60
	}

	start := hour*60 + minutes
	end := start + durationMinutes

	franja := findFranjaForInterval(resolved.Franjas, start, end)
	if franja == nil {
		return nil, true, nil
	}
	return franja, true, nil
}

// findFranjaForInterval retorna la franja que contiene completamente el
// intervalo [start, end] en minutos. Devuelve nil si ninguna la cubre.
func findFranjaForInterval(franjas []domain.ScheduleRuleSlot, start, end int) *domain.ScheduleRuleSlot {
	for i := range franjas {
		f := &franjas[i]
		fs, err := timeToMinutes(f.StartTime)
		if err != nil {
			continue
		}
		fe, err := timeToMinutes(f.EndTime)
		if err != nil {
			continue
		}
		if start >= fs && end <= fe {
			return f
		}
	}
	return nil
}

// mostSpecificRuleForDay elige la regla aplicable al día con menos días
// (la más específica gana sobre la general).
func mostSpecificRuleForDay(rules []domain.ScheduleRule, weekday int) *domain.ScheduleRule {
	var best *domain.ScheduleRule
	for i := range rules {
		r := &rules[i]
		if !containsDay(r.DaysOfWeek, weekday) {
			continue
		}
		if best == nil || len(r.DaysOfWeek) < len(best.DaysOfWeek) {
			best = r
		}
	}
	return best
}

func containsDay(days []int, d int) bool {
	for _, x := range days {
		if x == d {
			return true
		}
	}
	return false
}
