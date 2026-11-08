package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/logger"
)

var ErrInboxMessageNotFound = errors.New("inbox message not found")

const (
	inboxDefaultLimit = 30
	inboxMaxLimit     = 100
)

type InboxRepository interface {
	// Insert guarda el mensaje. Devuelve false si ya existía uno con el mismo
	// DedupeKey para el usuario (webhook reintentado, job repetido).
	Insert(ctx context.Context, msg *domain.InboxMessage) (bool, error)
	// List devuelve mensajes del usuario, más nuevos primero. before es un id
	// hex opcional (cursor): solo mensajes anteriores a él.
	List(ctx context.Context, userID, before string, limit int) ([]domain.InboxMessage, error)
	CountUnread(ctx context.Context, userID string) (int64, error)
	MarkRead(ctx context.Context, userID, id string) (bool, error)
	MarkAllRead(ctx context.Context, userID string) error
	Delete(ctx context.Context, userID, id string) (bool, error)
}

type InboxUseCase struct {
	repo InboxRepository
}

func NewInboxUseCase(repo InboxRepository) *InboxUseCase {
	return &InboxUseCase{repo: repo}
}

func (uc *InboxUseCase) List(ctx context.Context, userID, before string, limit int) ([]domain.InboxMessage, error) {
	if limit <= 0 {
		limit = inboxDefaultLimit
	}
	if limit > inboxMaxLimit {
		limit = inboxMaxLimit
	}
	msgs, err := uc.repo.List(ctx, userID, before, limit)
	if msgs == nil {
		msgs = []domain.InboxMessage{}
	}
	return msgs, err
}

func (uc *InboxUseCase) UnreadCount(ctx context.Context, userID string) (int64, error) {
	return uc.repo.CountUnread(ctx, userID)
}

func (uc *InboxUseCase) MarkRead(ctx context.Context, userID, id string) error {
	ok, err := uc.repo.MarkRead(ctx, userID, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInboxMessageNotFound
	}
	return nil
}

func (uc *InboxUseCase) MarkAllRead(ctx context.Context, userID string) error {
	return uc.repo.MarkAllRead(ctx, userID)
}

func (uc *InboxUseCase) Delete(ctx context.Context, userID, id string) error {
	ok, err := uc.repo.Delete(ctx, userID, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInboxMessageNotFound
	}
	return nil
}

// SetInbox inyecta la bandeja del cliente. Sin ella no se generan mensajes.
func (uc *BookingUseCase) SetInbox(repo InboxRepository) {
	uc.inbox = repo
}

// notifyCustomer deja un aviso en la bandeja del usuario dueño de la reserva y
// envía la push correspondiente. Las reservas de invitado (sin UserID) no
// tienen bandeja, pero sí push si registraron su dispositivo.
//
// Nunca falla la operación de negocio: los errores solo se registran.
func (uc *BookingUseCase) notifyCustomer(ctx context.Context, booking *domain.Booking, msgType, title, body string) {
	if booking == nil {
		return
	}

	log := logger.FromContext(ctx)

	// La bandeja solo existe para usuarios autenticados. Su dedupe (tipo +
	// reserva + horario) también evita la push duplicada cuando el webhook
	// reintenta, y permite un aviso nuevo si la reserva se mueve.
	if uc.inbox != nil && booking.UserID != "" {
		msg := &domain.InboxMessage{
			UserID:    booking.UserID,
			Type:      msgType,
			Title:     title,
			Body:      body,
			DedupeKey: notificationDedupeKey(booking, msgType),
			Data: map[string]string{
				"booking_id":      booking.ID.Hex(),
				"booking_code":    booking.BookingCode,
				"sport_center_id": booking.SportCenterID.Hex(),
			},
		}
		inserted, err := uc.inbox.Insert(ctx, msg)
		if err != nil {
			log.Warnw("inbox_insert_failed", "error", err, "booking_id", booking.ID.Hex())
		} else if !inserted {
			return
		}
	}

	if uc.push == nil {
		return
	}
	uc.push.NotifyCustomer(ctx, booking, PushEvent{
		Type:  msgType,
		Title: title,
		Body:  body,
	})
}

// ReminderBookingFinder lista reservas confirmadas con inicio en [from, to).
// Incluye reservas de invitados que registraron su dispositivo.
type ReminderBookingFinder interface {
	FindConfirmedWithUserBetween(ctx context.Context, from, to time.Time) ([]domain.Booking, error)
}

// ReminderSummary resume una ejecución del job de recordatorios.
type ReminderSummary struct {
	Scanned int `json:"scanned"`
	Sent    int `json:"sent"`
	Skipped int `json:"skipped"`
}

// SetReminderFinder inyecta el origen de reservas para los recordatorios.
func (uc *BookingUseCase) SetReminderFinder(f ReminderBookingFinder) {
	uc.reminders = f
}

// SendBookingReminders envía un recordatorio push (y deja uno en la bandeja)
// para las reservas creadas en la app mobile que empiezan dentro de [lead]. Es
// idempotente por reserva + horario, por lo que puede ejecutarse cada pocos
// minutos y volver a avisar si la reserva cambia de hora. No avisa de reservas
// hechas dentro de la propia ventana: el usuario acaba de crearlas.
func (uc *BookingUseCase) SendBookingReminders(ctx context.Context, now time.Time, lead time.Duration) (*ReminderSummary, error) {
	summary := &ReminderSummary{}
	if uc.reminders == nil {
		return summary, errors.New("booking reminders are not configured")
	}
	if uc.inbox == nil && uc.push == nil {
		return summary, errors.New("booking reminders are not configured")
	}
	bookings, err := uc.reminders.FindConfirmedWithUserBetween(ctx, now, now.Add(lead))
	if err != nil {
		return summary, err
	}
	loc := domain.GetSantiagoLocation()
	for i := range bookings {
		b := &bookings[i]
		summary.Scanned++
		// El finder ya filtra por estado, pero el recordatorio es best-effort:
		// una reserva cancelada entre la consulta y el envío no debe avisarse.
		if b.Status != domain.BookingStatusConfirmed ||
			b.BookingSource != domain.BookingSourceMobile {
			summary.Skipped++
			continue
		}
		start := b.StartInstant()
		if !b.CreatedAt.IsZero() && b.CreatedAt.After(start.Add(-lead)) {
			summary.Skipped++
			continue
		}
		if b.Timezone != "" {
			if l, err := time.LoadLocation(b.Timezone); err == nil {
				loc = l
			}
		}
		local := start.In(loc)
		nowLocal := now.In(loc)
		day := local.Format("el 02/01/2006")
		switch local.Format("2006-01-02") {
		case nowLocal.Format("2006-01-02"):
			day = "hoy"
		case nowLocal.AddDate(0, 0, 1).Format("2006-01-02"):
			day = "mañana"
		}
		body := fmt.Sprintf("Tu reserva en %s empieza %s a las %s hrs.", b.SportCenterName, day, local.Format("15:04"))
		if b.CourtName != "" {
			body = fmt.Sprintf("Tu reserva en %s (%s) empieza %s a las %s hrs.", b.SportCenterName, b.CourtName, day, local.Format("15:04"))
		}
		uc.notifyCustomer(ctx, b, domain.InboxBookingReminder, "Recordatorio de tu reserva", body)
		summary.Sent++
	}
	return summary, nil
}
