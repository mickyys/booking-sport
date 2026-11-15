package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestPaymentDeadline_BaseWindow(t *testing.T) {
	anchor := time.Date(2026, 11, 13, 21, 0, 0, 0, time.UTC)
	// Entra a los 5 minutos: se le respeta la ventana base.
	got := paymentDeadline(anchor, anchor.Add(5*time.Minute))
	want := anchor.Add(mpHoldTTL)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPaymentDeadline_ExtendsAfterBorder(t *testing.T) {
	anchor := time.Date(2026, 11, 13, 21, 0, 0, 0, time.UTC)
	// Entra a los 14 minutos: se extiende hasta el tope (20 min).
	got := paymentDeadline(anchor, anchor.Add(14*time.Minute))
	want := anchor.Add(mpHoldTTL + mpPaymentExtension)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPaymentDeadline_NeverExceedsMax(t *testing.T) {
	anchor := time.Date(2026, 11, 13, 21, 0, 0, 0, time.UTC)
	max := anchor.Add(mpHoldTTL + mpPaymentExtension)
	for _, elapsed := range []time.Duration{10 * time.Minute, 15 * time.Minute, 19 * time.Minute, 25 * time.Minute} {
		got := paymentDeadline(anchor, anchor.Add(elapsed))
		if got.After(max) {
			t.Fatalf("elapsed %v: %v excede el tope %v", elapsed, got, max)
		}
	}
}

func TestHoldDeadline_AddsGrace(t *testing.T) {
	anchor := time.Date(2026, 11, 13, 21, 0, 0, 0, time.UTC)
	got := holdDeadline(anchor, anchor)
	want := anchor.Add(mpHoldTTL + mpHoldGrace)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSlotInPaymentError_ReportsRemainingTime(t *testing.T) {
	now := time.Date(2026, 11, 13, 21, 50, 0, 0, time.UTC)
	lockEnd := now.Add(8 * time.Minute)
	err := slotInPaymentError(lockEnd, now)

	if err.StatusCode != 409 {
		t.Fatalf("expected 409, got %d", err.StatusCode)
	}
	if !strings.Contains(err.Message, "otro usuario esta en proceso de pago") {
		t.Errorf("mensaje inesperado: %q", err.Message)
	}
	if !strings.Contains(err.Message, "8 minutos") {
		t.Errorf("el mensaje debe indicar el tiempo restante: %q", err.Message)
	}
	if got := err.Details["code"]; got != "slot_in_payment" {
		t.Errorf("code = %v", got)
	}
	if got := err.Details["retry_after_seconds"]; got != 480 {
		t.Errorf("retry_after_seconds = %v, want 480", got)
	}
	if got, ok := err.Details["lock_expires_at"].(string); !ok || got == "" {
		t.Errorf("lock_expires_at ausente o inválido: %v", err.Details["lock_expires_at"])
	}
}

func TestSlotInPaymentError_LessThanOneMinute(t *testing.T) {
	now := time.Date(2026, 11, 13, 21, 50, 0, 0, time.UTC)
	err := slotInPaymentError(now.Add(30*time.Second), now)
	if !strings.Contains(err.Message, "menos de un minuto") {
		t.Errorf("mensaje inesperado: %q", err.Message)
	}
	if got := err.Details["retry_after_seconds"]; got != 30 {
		t.Errorf("retry_after_seconds = %v, want 30", got)
	}
}

func TestPaymentAnchor_FallsBackToLock(t *testing.T) {
	now := time.Now()
	lock := now.Add(-time.Hour)
	b := &domain.Booking{LockExpiresAt: &lock}
	got := paymentAnchor(b, now)
	want := lock.Add(-(mpHoldTTL + mpHoldGrace))
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestClaimOrRenewSlot_ExtendsLockForSameUser(t *testing.T) {
	courtID := newObjectID()
	date := chileDate(2026, 5, 13)
	hour := 10
	userID := "device:abc"

	createdAt := time.Now().Add(-12 * time.Minute)
	oldLock := createdAt.Add(mpHoldTTL + mpHoldGrace)
	pending := &domain.Booking{
		ID:            newObjectID(),
		GuestDeviceID: userID,
		CreatedAt:     createdAt,
		LockExpiresAt: &oldLock,
	}
	hold := &domain.SlotHold{
		ID:        newObjectID(),
		CourtID:   courtID,
		Hour:      hour,
		UserID:    userID,
		BookingID: pending.ID,
		ExpiresAt: oldLock,
	}

	var renewed time.Time
	bookingRepo := &mockBookingRepoForHold{
		FindPendingBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int, minutes int) (*domain.Booking, error) {
			return pending, nil
		},
		UpdateLockExpiresAtFn: func(ctx context.Context, id primitive.ObjectID, expiresAt time.Time) error {
			renewed = expiresAt
			return nil
		},
	}
	holdRepo := &mockSlotHoldRepo{
		FindByBookingIDFn: func(ctx context.Context, bookingID primitive.ObjectID) (*domain.SlotHold, error) {
			return hold, nil
		},
	}

	uc := &BookingUseCase{repo: bookingRepo, holdRepo: holdRepo}
	gotHold, gotBooking, err := uc.ClaimOrRenewSlot(context.Background(), courtID, date, hour, 0, userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBooking == nil || gotBooking.ID != pending.ID {
		t.Fatalf("expected the pending booking back, got %+v", gotBooking)
	}
	if gotHold == nil {
		t.Fatal("expected non-nil hold")
	}
	wantEnd := createdAt.Add(mpHoldTTL + mpPaymentExtension + mpHoldGrace)
	if !renewed.Equal(wantEnd) {
		t.Errorf("lock renovado a %v, esperaba %v", renewed, wantEnd)
	}
	if !gotHold.ExpiresAt.Equal(wantEnd) {
		t.Errorf("hold renovado a %v, esperaba %v", gotHold.ExpiresAt, wantEnd)
	}
}

func TestClaimOrRenewSlot_PaymentWindowClosedStartsNewAttempt(t *testing.T) {
	courtID := newObjectID()
	date := chileDate(2026, 5, 13)
	hour := 10
	userID := "device:abc"

	createdAt := time.Now().Add(-26 * time.Minute)
	oldLock := createdAt.Add(mpHoldTTL + mpHoldGrace)
	oldHoldID := newObjectID()
	pending := &domain.Booking{
		ID:            newObjectID(),
		GuestDeviceID: userID,
		CreatedAt:     createdAt,
		LockExpiresAt: &oldLock,
		HoldID:        &oldHoldID,
	}

	bookingRepo := &mockBookingRepoForHold{
		FindPendingBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int, minutes int) (*domain.Booking, error) {
			return pending, nil
		},
	}
	holdRepo := &mockSlotHoldRepo{
		TryClaimSlotFn: func(ctx context.Context, hold *domain.SlotHold) (*domain.SlotHold, error) {
			return hold, nil
		},
	}

	uc := &BookingUseCase{repo: bookingRepo, holdRepo: holdRepo}
	gotHold, gotBooking, err := uc.ClaimOrRenewSlot(context.Background(), courtID, date, hour, 0, userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBooking != nil {
		t.Errorf("la reserva vencida no debe reutilizarse, got %+v", gotBooking)
	}
	if gotHold == nil {
		t.Fatal("expected a new hold")
	}
	if len(bookingRepo.MarkExpiredCalls) != 1 || bookingRepo.MarkExpiredCalls[0] != pending.ID {
		t.Errorf("expected MarkExpired for %s, got %v", pending.ID.Hex(), bookingRepo.MarkExpiredCalls)
	}
	if len(holdRepo.DeleteCalls) != 1 || holdRepo.DeleteCalls[0] != oldHoldID {
		t.Errorf("expected Delete for %s, got %v", oldHoldID.Hex(), holdRepo.DeleteCalls)
	}
	// Intento nuevo: ventana base + gracia.
	wantMin := time.Now().Add(mpHoldTTL + mpHoldGrace - time.Minute)
	if gotHold.ExpiresAt.Before(wantMin) {
		t.Errorf("hold nuevo vence demasiado pronto: %v", gotHold.ExpiresAt)
	}
}

func TestClaimOrRenewSlot_OtherUserSeesRemainingTime(t *testing.T) {
	courtID := newObjectID()
	date := chileDate(2026, 5, 13)
	hour := 10

	createdAt := time.Now().Add(-12 * time.Minute)
	lock := createdAt.Add(mpHoldTTL + mpHoldGrace)
	pending := &domain.Booking{
		ID:            newObjectID(),
		GuestDeviceID: "device:other",
		UserID:        "auth0|other",
		CreatedAt:     createdAt,
		LockExpiresAt: &lock,
	}

	bookingRepo := &mockBookingRepoForHold{
		FindPendingBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int, minutes int) (*domain.Booking, error) {
			return pending, nil
		},
	}
	uc := &BookingUseCase{repo: bookingRepo, holdRepo: &mockSlotHoldRepo{}}

	_, _, err := uc.ClaimOrRenewSlot(context.Background(), courtID, date, hour, 0, "device:abc")
	if err == nil {
		t.Fatal("expected conflict")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if !strings.Contains(appErr.Message, "otro usuario esta en proceso de pago") {
		t.Errorf("mensaje inesperado: %q", appErr.Message)
	}
	if _, ok := appErr.Details["retry_after_seconds"].(int); !ok {
		t.Errorf("falta retry_after_seconds en %v", appErr.Details)
	}
}

// En la gracia el link ya no acepta pagos, pero el horario sigue bloqueado:
// otro usuario debe ver cuánto falta y el dueño no puede reiniciar el pago.
func TestClaimOrRenewSlot_GraceStillBlocksOtherUser(t *testing.T) {
	courtID := newObjectID()
	date := chileDate(2026, 5, 13)
	hour := 10

	createdAt := time.Now().Add(-22 * time.Minute)
	lock := createdAt.Add(mpHoldTTL + mpPaymentExtension + mpHoldGrace)
	pending := &domain.Booking{
		ID:            newObjectID(),
		GuestDeviceID: "device:other",
		UserID:        "auth0|other",
		CreatedAt:     createdAt,
		LockExpiresAt: &lock,
	}

	bookingRepo := &mockBookingRepoForHold{
		FindPendingBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int, minutes int) (*domain.Booking, error) {
			return pending, nil
		},
	}
	uc := &BookingUseCase{repo: bookingRepo, holdRepo: &mockSlotHoldRepo{}}

	_, _, err := uc.ClaimOrRenewSlot(context.Background(), courtID, date, hour, 0, "device:abc")
	if err == nil {
		t.Fatal("expected conflict")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if got := appErr.Details["code"]; got != "slot_in_payment" {
		t.Errorf("code = %v, want slot_in_payment", got)
	}
	remaining, _ := appErr.Details["retry_after_seconds"].(int)
	if remaining <= 0 || remaining > int(mpHoldGrace/time.Second) {
		t.Errorf("retry_after_seconds = %v, esperaba hasta %v", remaining, mpHoldGrace)
	}
	if !strings.Contains(appErr.Message, "se libera") {
		t.Errorf("el mensaje debe indicar cuándo se libera: %q", appErr.Message)
	}
}

// El mismo cliente que vuelve durante la gracia no puede reabrir el pago ni
// liberar el horario antes de tiempo.
func TestClaimOrRenewSlot_GraceBlocksOwnerWithoutRenewing(t *testing.T) {
	courtID := newObjectID()
	date := chileDate(2026, 5, 13)
	hour := 10
	userID := "device:abc"

	createdAt := time.Now().Add(-22 * time.Minute)
	lock := createdAt.Add(mpHoldTTL + mpPaymentExtension + mpHoldGrace)
	pending := &domain.Booking{
		ID:            newObjectID(),
		GuestDeviceID: userID,
		CreatedAt:     createdAt,
		LockExpiresAt: &lock,
	}

	bookingRepo := &mockBookingRepoForHold{
		FindPendingBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int, minutes int) (*domain.Booking, error) {
			return pending, nil
		},
	}
	holdRepo := &mockSlotHoldRepo{}
	uc := &BookingUseCase{repo: bookingRepo, holdRepo: holdRepo}

	_, _, err := uc.ClaimOrRenewSlot(context.Background(), courtID, date, hour, 0, userID)
	if err == nil {
		t.Fatal("expected conflict")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if got := appErr.Details["code"]; got != "payment_window_expired" {
		t.Errorf("code = %v, want payment_window_expired", got)
	}
	if !strings.Contains(appErr.Message, "tu tiempo para pagar venció") {
		t.Errorf("mensaje inesperado: %q", appErr.Message)
	}
	if got := appErr.Details["lock_expires_at"]; got == "" {
		t.Error("falta lock_expires_at")
	}
	// No debe marcar la reserva como expirada ni renovar el bloqueo.
	if len(bookingRepo.MarkExpiredCalls) != 0 {
		t.Errorf("MarkExpired inesperado: %v", bookingRepo.MarkExpiredCalls)
	}
	if holdRepo.RenewExpirationCalls != 0 || holdRepo.TryClaimSlotCalls != 0 {
		t.Errorf("no debía tocar el bloqueo: renew=%d claim=%d", holdRepo.RenewExpirationCalls, holdRepo.TryClaimSlotCalls)
	}
}
