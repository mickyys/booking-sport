package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/logger"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	defaultRecurringHorizonWeeks = 4
	recurringJobLockName         = "recurring-generate"
	recurringJobLockTTL          = 10 * time.Minute
)

// RecurringGenerationSummary es el resultado de una corrida del generador de
// ocurrencias. Se expone en el endpoint interno que invoca el batch.
type RecurringGenerationSummary struct {
	Scanned       int   `json:"scanned"`
	Created       int   `json:"created"`
	AlreadyExists int   `json:"already_exists"`
	Skipped       int   `json:"skipped"`
	Failed        int   `json:"failed"`
	SeriesErrors  int   `json:"series_errors"`
	Locked        bool  `json:"locked"`
	DurationMS    int64 `json:"duration_ms"`
}

type recurringSeriesResult struct {
	Created       int
	AlreadyExists int
	Skipped       int
	Failed        int
	Issues        []domain.RecurringGenerationIssue

	// FirstTargetOK indica si la primera ocurrencia objetivo quedó cubierta
	// (creada o ya existente). Se usa al crear una recurrencia para revertirla
	// si su primera reserva no pudo generarse.
	FirstTargetAttempted bool
	FirstTargetOK        bool
	FirstTargetReason    string
}

func (r *recurringSeriesResult) addIssue(date, reason string) {
	if len(r.Issues) >= 12 {
		return
	}
	r.Issues = append(r.Issues, domain.RecurringGenerationIssue{Date: date, Reason: reason})
}

func (r *recurringSeriesResult) errorSummary() string {
	if len(r.Issues) == 0 {
		return ""
	}
	parts := make([]string, 0, len(r.Issues))
	for _, issue := range r.Issues {
		parts = append(parts, fmt.Sprintf("%s: %s", issue.Date, issue.Reason))
	}
	return strings.Join(parts, "; ")
}

// GenerateUpcomingRecurringBookings mantiene las próximas ocurrencias de todas
// las recurrencias activas. Es idempotente: si una ocurrencia ya existe no se
// vuelve a crear. Las fechas con conflicto, bloqueo, feriado, retención o pago
// pendiente se saltan y se registran, sin compensar con una quinta fecha.
func (uc *BookingUseCase) GenerateUpcomingRecurringBookings(ctx context.Context) (*RecurringGenerationSummary, error) {
	startedAt := time.Now()
	summary := &RecurringGenerationSummary{}

	if uc == nil || uc.repo == nil || uc.recurringReservationRepo == nil {
		return summary, fmt.Errorf("recurring booking generation is not configured")
	}

	owner := primitive.NewObjectID().Hex()
	if uc.jobLockRepo != nil {
		acquired, err := uc.jobLockRepo.TryAcquire(ctx, recurringJobLockName, owner, recurringJobLockTTL, startedAt)
		if err != nil {
			return summary, fmt.Errorf("acquire recurring job lock: %w", err)
		}
		if !acquired {
			summary.Locked = true
			summary.DurationMS = time.Since(startedAt).Milliseconds()
			return summary, nil
		}
		defer func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = uc.jobLockRepo.Release(releaseCtx, recurringJobLockName, owner)
		}()
	}

	reservations, err := uc.recurringReservationRepo.FindAllActive(ctx)
	if err != nil {
		return summary, fmt.Errorf("find active recurring reservations: %w", err)
	}
	summary.Scanned = len(reservations)

	log := logger.FromContext(ctx)
	now := time.Now()

	for i := range reservations {
		reservation := &reservations[i]
		result := uc.generateForRecurring(ctx, reservation, now)

		summary.Created += result.Created
		summary.AlreadyExists += result.AlreadyExists
		summary.Skipped += result.Skipped
		summary.Failed += result.Failed

		ok := result.Failed == 0
		errMsg := ""
		if !ok {
			summary.SeriesErrors++
			errMsg = result.errorSummary()
		}
		if err := uc.recurringReservationRepo.UpdateGenerationState(ctx, reservation.ID, now, ok, errMsg, result.Created, result.Issues); err != nil {
			log.Warnw("recurring_generation_state_update_failed",
				"msg", "no se pudo guardar el estado de generación de la recurrencia",
				"error", err,
				"recurring_id", reservation.ID.Hex(),
			)
		}

		if result.Created > 0 || result.Skipped > 0 || result.Failed > 0 {
			log.Infow("recurring_generation_series_done",
				"msg", "Ocurrencias evaluadas para la recurrencia",
				"recurring_id", reservation.ID.Hex(),
				"court_id", reservation.CourtID.Hex(),
				"day_of_week", reservation.DayOfWeek,
				"hour", reservation.Hour,
				"minutes", reservation.Minutes,
				"created", result.Created,
				"already_exists", result.AlreadyExists,
				"skipped", result.Skipped,
				"failed", result.Failed,
			)
		}
	}

	summary.DurationMS = time.Since(startedAt).Milliseconds()
	return summary, nil
}

// generateForRecurring calcula y crea hasta HorizonWeeks ocurrencias futuras
// para una serie. No envía correos ni notificaciones: la generación es interna
// y silenciosa.
func (uc *BookingUseCase) generateForRecurring(ctx context.Context, reservation *domain.RecurringReservation, now time.Time) recurringSeriesResult {
	result := recurringSeriesResult{}
	if uc == nil || uc.repo == nil || reservation == nil {
		return result
	}

	loc := domain.GetSantiagoLocation()
	nowLoc := now.In(loc)
	today := domain.SantiagoCivilDate(nowLoc)

	horizon := reservation.HorizonWeeks
	if horizon <= 0 {
		horizon = defaultRecurringHorizonWeeks
	}
	if reservation.DayOfWeek < 0 || reservation.DayOfWeek > 6 {
		result.Failed++
		result.FirstTargetAttempted = true
		result.FirstTargetReason = "día de la semana inválido"
		result.addIssue(today.Format("2006-01-02"), "día de la semana inválido")
		return result
	}

	base := today
	if start := reservation.EffectiveStartDate(); !start.IsZero() && start.After(base) {
		base = start
	}

	first, ok := firstRecurringOccurrence(base, reservation.DayOfWeek)
	if !ok {
		result.Failed++
		result.FirstTargetAttempted = true
		result.FirstTargetReason = "no se pudo calcular la próxima ocurrencia"
		result.addIssue(base.Format("2006-01-02"), result.FirstTargetReason)
		return result
	}

	// Si la primera ocurrencia es hoy pero el horario ya pasó, la serie
	// comienza la próxima semana.
	firstSlot := time.Date(first.Year(), first.Month(), first.Day(), reservation.Hour, reservation.Minutes, 0, 0, loc)
	if !firstSlot.After(nowLoc) {
		first = first.AddDate(0, 0, 7)
	}

	duration := reservation.DurationMinutes
	if duration <= 0 {
		duration = 60
	}

	result.FirstTargetAttempted = true

	var court *domain.Court
	var center *domain.SportCenter
	courtLoaded := false
	centerLoaded := false

	for i := 0; i < horizon; i++ {
		target := first.AddDate(0, 0, 7*i)
		dateStr := target.Format("2006-01-02")
		isFirst := i == 0
		markFirst := func(ok bool, reason string) {
			if !isFirst {
				return
			}
			result.FirstTargetOK = ok
			result.FirstTargetReason = reason
		}

		if target.Before(today) {
			result.Skipped++
			result.addIssue(dateStr, "fecha pasada")
			markFirst(false, "fecha pasada")
			continue
		}
		if !reservation.CoversDate(target) {
			continue
		}
		if reservation.IsDateCancelled(target) {
			result.Skipped++
			result.addIssue(dateStr, "fecha anulada en la recurrencia")
			// Una fecha anulada intencionalmente no invalida la creación de la serie.
			markFirst(true, "fecha anulada en la recurrencia")
			continue
		}

		existing, err := uc.repo.FindByRecurringIDAndDate(ctx, reservation.ID.Hex(), target)
		if err == nil && existing != nil {
			result.AlreadyExists++
			markFirst(true, "ocurrencia ya existente")
			continue
		}
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			result.Failed++
			result.addIssue(dateStr, "error consultando ocurrencia: "+err.Error())
			markFirst(false, "error consultando ocurrencia")
			continue
		}

		if !courtLoaded {
			courtLoaded = true
			court, err = uc.courtRepo.FindByID(ctx, reservation.CourtID)
			if err != nil {
				court = nil
			}
		}
		if court == nil {
			result.Failed++
			result.addIssue(dateStr, "cancha no encontrada")
			markFirst(false, "cancha no encontrada")
			continue
		}

		if !centerLoaded {
			centerLoaded = true
			center, err = uc.centerRepo.FindByID(ctx, court.SportCenterID)
			if err != nil {
				center = nil
			}
		}
		if center == nil {
			result.Failed++
			result.addIssue(dateStr, "centro deportivo no encontrado")
			markFirst(false, "centro deportivo no encontrado")
			continue
		}

		price, err := uc.resolveRecurringSlotPrice(ctx, court, target, reservation.Hour, reservation.Minutes, duration)
		if err != nil {
			result.Skipped++
			result.addIssue(dateStr, "horario no disponible: "+err.Error())
			markFirst(false, "horario no disponible")
			continue
		}

		finalPrice := reservation.Price
		if finalPrice <= 0 {
			finalPrice = price
		}

		booking := domain.Booking{
			CourtID:         reservation.CourtID,
			SportCenterID:   court.SportCenterID,
			CourtName:       court.Name,
			SportCenterName: center.Name,
			Date:            target,
			Hour:            reservation.Hour,
			Minutes:         reservation.Minutes,
			DurationMinutes: duration,
			Price:           finalPrice,
			FinalPrice:      finalPrice,
			Status:          domain.BookingStatusConfirmed,
			PaymentMethod:   "internal",
			CustomerName:    reservation.CustomerName,
			CustomerPhone:   reservation.CustomerPhone,
			// Las ocurrencias generadas no llevan SeriesID para que no se
			// dupliquen como "serie" en el panel de recurrencias. RecurringID
			// las vincula a su serie semanal.
			RecurringID: reservation.ID.Hex(),
			BookingCode: generateBookingCode(),
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		conflicting, _ := uc.repo.FindConfirmedBySlot(ctx, reservation.CourtID, target, reservation.Hour, reservation.Minutes)
		if conflicting != nil {
			result.Skipped++
			result.addIssue(dateStr, "horario ocupado por otra reserva")
			markFirst(false, "horario ocupado por otra reserva")
			continue
		}

		if err := uc.checkConfirmedIntervalConflict(ctx, &booking, reservation.Minutes); err != nil {
			result.Skipped++
			result.addIssue(dateStr, "conflicto de superposición: "+err.Error())
			markFirst(false, "conflicto de superposición")
			continue
		}

		if uc.holdRepo != nil {
			hold, _ := uc.holdRepo.FindBySlot(ctx, reservation.CourtID, target, reservation.Hour, reservation.Minutes)
			if hold != nil && nowLoc.Before(hold.ExpiresAt) {
				result.Skipped++
				result.addIssue(dateStr, "horario retenido por otro usuario")
				markFirst(false, "horario retenido por otro usuario")
				continue
			}
		}

		pending, _ := uc.repo.FindPendingBySlot(ctx, reservation.CourtID, target, reservation.Hour, reservation.Minutes)
		if pending != nil {
			if pending.LockExpiresAt == nil || nowLoc.After(*pending.LockExpiresAt) {
				if pending.HoldID != nil && uc.holdRepo != nil {
					_ = uc.holdRepo.Delete(ctx, *pending.HoldID)
				}
				_ = uc.repo.MarkExpired(ctx, pending.ID)
			} else {
				result.Skipped++
				result.addIssue(dateStr, "pago pendiente de otro usuario")
				markFirst(false, "pago pendiente de otro usuario")
				continue
			}
		}

		if err := uc.repo.CreateRecurringBooking(ctx, &booking); err != nil {
			if errors.Is(err, domain.ErrBookingSlotTaken) {
				result.AlreadyExists++
				markFirst(false, "horario ocupado por otra reserva")
				continue
			}
			// Los cambios de horario (DST) pueden hacer que una hora local no
			// exista o sea ambigua esa semana. Se salta la fecha.
			var nonexistent *domain.NonexistentLocalTimeError
			var ambiguous *domain.AmbiguousLocalTimeError
			if errors.As(err, &nonexistent) || errors.As(err, &ambiguous) {
				result.Skipped++
				result.addIssue(dateStr, "horario no existe por cambio de hora")
				markFirst(false, "horario no existe por cambio de hora")
				continue
			}
			result.Failed++
			result.addIssue(dateStr, "error creando reserva: "+err.Error())
			markFirst(false, "error creando reserva")
			continue
		}

		result.Created++
		markFirst(true, "")
	}

	return result
}

// firstRecurringOccurrence devuelve la primera fecha (a las 12:00 de Santiago)
// mayor o igual a base cuyo día de la semana coincide con dayOfWeek.
func firstRecurringOccurrence(base time.Time, dayOfWeek int) (time.Time, bool) {
	if dayOfWeek < 0 || dayOfWeek > 6 {
		return time.Time{}, false
	}
	civil := domain.SantiagoCivilDate(base)
	for i := 0; i < 7; i++ {
		candidate := civil.AddDate(0, 0, i)
		if int(candidate.Weekday()) == dayOfWeek {
			return candidate, true
		}
	}
	return time.Time{}, false
}

// resolveRecurringSlotPrice valida que el horario exista y esté disponible,
// devolviendo el precio configurado. Respeta el modelo de reglas (incluyendo
// excepciones y bloqueos) y el horario legacy (incluyendo franjas cerradas).
func (uc *BookingUseCase) resolveRecurringSlotPrice(ctx context.Context, court *domain.Court, date time.Time, hour, minutes, duration int) (float64, error) {
	if duration <= 0 {
		duration = 60
	}
	if uc.scheduleResolver != nil {
		resolved, err := uc.scheduleResolver.ResolveFranjasForDate(ctx, court.ID, date)
		if err != nil {
			return 0, err
		}
		if resolved.HasRules {
			start := hour*60 + minutes
			end := start + duration
			for _, block := range resolved.Blocks {
				blockStart, errStart := timeToMinutes(block.StartTime)
				blockEnd, errEnd := timeToMinutes(block.EndTime)
				if errStart != nil || errEnd != nil {
					continue
				}
				if start < blockEnd && blockStart < end {
					reason := block.Reason
					if reason == "" {
						reason = "bloqueo"
					}
					return 0, fmt.Errorf("horario bloqueado: %s", reason)
				}
			}
			if resolved.Exception != nil && resolved.Exception.Type == domain.ScheduleExceptionClosed {
				return 0, fmt.Errorf("el centro está cerrado en esa fecha")
			}
			franja := findFranjaForInterval(resolved.Franjas, start, end)
			if franja == nil {
				return 0, fmt.Errorf("el horario %02d:%02d no está dentro de la configuración de la cancha", hour, minutes)
			}
			return franja.Price, nil
		}
	}
	for _, slot := range court.Schedule {
		if slot.Hour == hour && slot.Minutes == minutes {
			if slot.Status != "" && slot.Status != "available" {
				return 0, fmt.Errorf("franja cerrada o no disponible")
			}
			return slot.Price, nil
		}
	}
	return 0, fmt.Errorf("hour %02d:%02d not found in schedule", hour, minutes)
}
