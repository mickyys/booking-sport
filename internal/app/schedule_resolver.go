package app

import (
	"context"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/sync/errgroup"
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

	searchDate := santiagoMidnight(date)

	ex, err := r.exceptionRepo.FindBySpaceIDAndDate(ctx, spaceID, searchDate)
	if err != nil {
		return nil, err
	}

	blocks, err := r.blockRepo.FindBySpaceIDAndDate(ctx, spaceID, searchDate)
	if err != nil {
		return nil, err
	}
	return resolveFranjas(rules, ex, blocks, searchDate), nil
}

// resolveFranjas combina reglas, excepción y bloqueos ya cargados. No hace
// consultas: es la parte compartida entre la resolución por espacio y la
// resolución por lote de un centro completo.
func resolveFranjas(rules []domain.ScheduleRule, ex *domain.ScheduleException, blocks []domain.Block, searchDate time.Time) *ResolvedSchedule {
	res := &ResolvedSchedule{HasRules: true}

	if ex != nil {
		res.Exception = ex
		if ex.Type == domain.ScheduleExceptionSpecial {
			res.Franjas = ex.Slots
		}
		// closed: sin disponibilidad (Franjas queda nil)
	} else if rule := mostSpecificRuleForDay(rules, int(searchDate.Weekday())); rule != nil {
		res.Franjas = rule.Slots
	}

	res.Blocks = blocks
	return res
}

// CenterScheduleData agrupa las reglas, excepciones y bloqueos de todos los
// espacios de un centro para una fecha concreta. Se carga una sola vez por
// request para no repetir 3 consultas por espacio.
type CenterScheduleData struct {
	rules      map[primitive.ObjectID][]domain.ScheduleRule
	exceptions map[primitive.ObjectID]*domain.ScheduleException
	blocks     map[primitive.ObjectID][]domain.Block
	date       time.Time
}

// Resolve retorna la disponibilidad del espacio con los datos ya cargados.
// Devuelve nil si no hay datos (el caller debe usar el modelo legacy).
func (d *CenterScheduleData) Resolve(spaceID primitive.ObjectID) *ResolvedSchedule {
	if d == nil {
		return nil
	}
	rules := d.rules[spaceID]
	if len(rules) == 0 {
		return &ResolvedSchedule{HasRules: false}
	}
	return resolveFranjas(rules, d.exceptions[spaceID], d.blocks[spaceID], d.date)
}

// LoadCenterScheduleData carga las reglas, excepciones y bloqueos del centro
// para `date` con 3 consultas en paralelo (en vez de 3 por espacio).
func (r *ScheduleResolver) LoadCenterScheduleData(ctx context.Context, centerID primitive.ObjectID, date time.Time) (*CenterScheduleData, error) {
	searchDate := santiagoMidnight(date)

	var (
		rules      []domain.ScheduleRule
		exceptions []domain.ScheduleException
		blocks     []domain.Block
	)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		rules, err = r.ruleRepo.FindBySportCenterID(gctx, centerID)
		return err
	})
	g.Go(func() error {
		var err error
		exceptions, err = r.exceptionRepo.FindBySportCenterID(gctx, centerID)
		return err
	})
	g.Go(func() error {
		var err error
		blocks, err = r.blockRepo.FindBySportCenterID(gctx, centerID)
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	data := &CenterScheduleData{
		rules:      make(map[primitive.ObjectID][]domain.ScheduleRule, len(rules)),
		exceptions: make(map[primitive.ObjectID]*domain.ScheduleException),
		blocks:     make(map[primitive.ObjectID][]domain.Block),
		date:       searchDate,
	}
	for _, rule := range rules {
		data.rules[rule.SpaceID] = append(data.rules[rule.SpaceID], rule)
	}
	for i := range exceptions {
		ex := exceptions[i]
		if !sameSantiagoDate(ex.Date, searchDate) {
			continue
		}
		if _, exists := data.exceptions[ex.SpaceID]; exists {
			continue
		}
		data.exceptions[ex.SpaceID] = &ex
	}
	for _, block := range blocks {
		if !sameSantiagoDate(block.Date, searchDate) {
			continue
		}
		data.blocks[block.SpaceID] = append(data.blocks[block.SpaceID], block)
	}
	return data, nil
}

// santiagoMidnight returns the start of the Santiago calendar day of `date`.
func santiagoMidnight(date time.Time) time.Time {
	loc := domain.GetSantiagoLocation()
	d := date.In(loc)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
}

// sameSantiagoDate reports whether both instants fall on the same Santiago
// calendar day.
func sameSantiagoDate(a, b time.Time) bool {
	loc := domain.GetSantiagoLocation()
	ay, am, ad := a.In(loc).Date()
	by, bm, bd := b.In(loc).Date()
	return ay == by && am == bm && ad == bd
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
