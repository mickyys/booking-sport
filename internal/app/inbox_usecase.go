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
	// Insert guarda el mensaje; si ya existe uno con el mismo DedupeKey para el
	// usuario no hace nada.
	Insert(ctx context.Context, msg *domain.InboxMessage) error
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

// notifyCustomer deja un aviso en la bandeja del usuario dueño de la reserva.
// Las reservas de invitado (sin UserID) se ignoran. Nunca falla la operación
// de negocio: los errores solo se registran.
func (uc *BookingUseCase) notifyCustomer(ctx context.Context, booking *domain.Booking, msgType, title, body string) {
	if uc.inbox == nil || booking == nil || booking.UserID == "" {
		return
	}
	msg := &domain.InboxMessage{
		UserID:    booking.UserID,
		Type:      msgType,
		Title:     title,
		Body:      body,
		DedupeKey: msgType + ":" + booking.ID.Hex(),
		Data: map[string]string{
			"booking_id":      booking.ID.Hex(),
			"booking_code":    booking.BookingCode,
			"sport_center_id": booking.SportCenterID.Hex(),
		},
	}
	if err := uc.inbox.Insert(ctx, msg); err != nil {
		logger.FromContext(ctx).Warnw("inbox_insert_failed", "error", err, "booking_id", booking.ID.Hex())
	}
}

// ReminderBookingFinder lista reservas confirmadas de usuarios con inicio en
// [from, to).
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

// SendBookingReminders deja un recordatorio en la bandeja de quienes tienen un
// partido que empieza dentro de [lead]. Es idempotente (clave booking+tipo),
// por lo que puede ejecutarse cada pocos minutos. No avisa de reservas hechas
// dentro de la propia ventana: el usuario acaba de crearlas.
func (uc *BookingUseCase) SendBookingReminders(ctx context.Context, now time.Time, lead time.Duration) (*ReminderSummary, error) {
	summary := &ReminderSummary{}
	if uc.reminders == nil || uc.inbox == nil {
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
		body := fmt.Sprintf("Tu reserva en %s empieza hoy a las %s hrs.", b.SportCenterName, local.Format("15:04"))
		if b.CourtName != "" {
			body = fmt.Sprintf("Tu reserva en %s (%s) empieza a las %s hrs.", b.SportCenterName, b.CourtName, local.Format("15:04"))
		}
		uc.notifyCustomer(ctx, b, domain.InboxBookingReminder, "Recordatorio de tu reserva", body)
		summary.Sent++
	}
	return summary, nil
}
