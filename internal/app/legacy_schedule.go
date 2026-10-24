package app

import (
	"fmt"
	"sort"

	"github.com/hamp/booking-sport/internal/domain"
)

// Este archivo centraliza la conversión del modelo legacy de horarios
// (`spaces.schedule`, `open_hour/close_hour`) al modelo de reglas semanales
// (`schedule_rules`). Es utilizado tanto por el fallback de lectura del panel
// de administración como por `cmd/migrate-schedule-rules` para evitar que
// ambas rutas diverjan.

// BuildRulesFromLegacyCourtSchedule agrupa los slots legacy por día de la
// semana y genera una regla por grupo. Los slots sin `day_of_week` se agrupan
// en una regla general para los 7 días.
func BuildRulesFromLegacyCourtSchedule(slots []domain.CourtSchedule) []domain.ScheduleRule {
	if len(slots) == 0 {
		return nil
	}

	type group struct {
		days  []int
		slots []domain.ScheduleRuleSlot
	}

	groups := make(map[string]*group)
	order := make([]string, 0, 8)

	for _, s := range slots {
		if s.Status == "closed" {
			continue
		}
		start := s.Hour*60 + s.Minutes
		end := start + 60
		// Un slot que termina después de medianoche no es representable como
		// franja del mismo día; se omite igual que en la migración.
		if start < 0 || end > 1440 {
			continue
		}

		key := "general"
		days := []int{0, 1, 2, 3, 4, 5, 6}
		if s.DayOfWeek != nil {
			if *s.DayOfWeek < 0 || *s.DayOfWeek > 6 {
				continue
			}
			key = fmt.Sprintf("day-%d", *s.DayOfWeek)
			days = []int{*s.DayOfWeek}
		}

		g, ok := groups[key]
		if !ok {
			g = &group{days: days}
			groups[key] = g
			order = append(order, key)
		}
		g.slots = append(g.slots, domain.ScheduleRuleSlot{
			StartTime:             formatLegacyMinutes(start),
			EndTime:               formatLegacyMinutes(end),
			Price:                 s.Price,
			PaymentRequired:       s.PaymentRequired,
			PaymentOptional:       s.PaymentOptional,
			PartialPaymentEnabled: s.PartialPaymentEnabled,
		})
	}

	rules := make([]domain.ScheduleRule, 0, len(order))
	for _, key := range order {
		g := groups[key]
		sort.Slice(g.slots, func(i, j int) bool {
			return g.slots[i].StartTime < g.slots[j].StartTime
		})
		rules = append(rules, domain.ScheduleRule{
			DaysOfWeek: append([]int(nil), g.days...),
			Slots:      g.slots,
			Active:     true,
		})
	}
	return rules
}

// BuildRulesFromLegacyFacility genera la regla de disponibilidad legacy para un
// quincho. El precio del quincho se configura en el espacio, no en la franja.
func BuildRulesFromLegacyFacility(openHour, closeHour int) []domain.ScheduleRule {
	if openHour <= 0 || openHour > 23 {
		openHour = 9
	}
	if closeHour < 1 || closeHour > 24 || closeHour <= openHour {
		closeHour = 22
		if closeHour <= openHour {
			closeHour = openHour + 1
		}
	}
	return []domain.ScheduleRule{
		{
			DaysOfWeek: []int{0, 1, 2, 3, 4, 5, 6},
			Slots: []domain.ScheduleRuleSlot{
				{
					StartTime: formatLegacyMinutes(openHour * 60),
					EndTime:   formatLegacyMinutes(closeHour * 60),
				},
			},
			Active: true,
		},
	}
}

// BuildFullDayRule genera una regla de día completo para sedes y piscinas.
func BuildFullDayRule() []domain.ScheduleRule {
	return []domain.ScheduleRule{
		{
			DaysOfWeek: []int{0, 1, 2, 3, 4, 5, 6},
			Slots: []domain.ScheduleRuleSlot{
				{StartTime: "00:00", EndTime: "24:00"},
			},
			Active: true,
		},
	}
}

// BuildRulesFromLegacySpace elige la conversión adecuada según la categoría
// del espacio. Retorna nil cuando no hay información legacy representable.
func BuildRulesFromLegacySpace(space *domain.Space) []domain.ScheduleRule {
	if space == nil {
		return nil
	}
	switch space.Category {
	case domain.SpaceCategoryCancha:
		return BuildRulesFromLegacyCourtSchedule(space.Schedule)
	case domain.SpaceCategoryQuincho:
		return BuildRulesFromLegacyFacility(space.OpenHour, space.CloseHour)
	case domain.SpaceCategorySede, domain.SpaceCategoryPiscina:
		return BuildFullDayRule()
	default:
		return nil
	}
}

func formatLegacyMinutes(minutes int) string {
	if minutes < 0 {
		minutes = 0
	}
	// 1440 == "24:00": centinela de fin de día (medianoche).
	if minutes >= 1440 {
		return "24:00"
	}
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}
