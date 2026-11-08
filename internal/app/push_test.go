package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ---------------------------------------------------------------------------
// Dobles de prueba
// ---------------------------------------------------------------------------

type fakeDeviceRepo struct {
	mu       sync.Mutex
	byUser   map[string][]domain.UserDevice
	byCenter map[string][]domain.UserDevice
	byGuest  map[string][]domain.UserDevice
	deleted  []string
}

func (f *fakeDeviceRepo) Upsert(context.Context, *domain.UserDevice) error { return nil }

func (f *fakeDeviceRepo) FindByUserIDAndApp(_ context.Context, userID, _ string) ([]domain.UserDevice, error) {
	return f.byUser[userID], nil
}

func (f *fakeDeviceRepo) FindBySportCenterIDAndApp(_ context.Context, centerID, _ string) ([]domain.UserDevice, error) {
	return f.byCenter[centerID], nil
}

func (f *fakeDeviceRepo) FindByGuestDeviceID(_ context.Context, guestDeviceID string) ([]domain.UserDevice, error) {
	return f.byGuest[guestDeviceID], nil
}

func (f *fakeDeviceRepo) DeleteByTokenAndApp(context.Context, string, string, string) error {
	return nil
}

func (f *fakeDeviceRepo) DeleteByTokens(_ context.Context, tokens []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, tokens...)
	return nil
}

type fakeSender struct {
	ch     chan NotificationMessage
	result PushResult
}

func (f *fakeSender) SendPushNotification(_ context.Context, msg NotificationMessage) (PushResult, error) {
	f.ch <- msg
	return f.result, nil
}

type fakeDeliveries struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (f *fakeDeliveries) Claim(_ context.Context, key, _ string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seen[key] {
		return false, nil
	}
	f.seen[key] = true
	return true, nil
}

func newTestBooking() *domain.Booking {
	return &domain.Booking{
		ID:              primitive.NewObjectID(),
		SportCenterID:   primitive.NewObjectID(),
		BookingCode:     "abc123",
		SportCenterName: "Andes",
		CourtName:       "Cancha 1",
		Date:            time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
		Hour:            20,
		Minutes:         0,
	}
}

func receive(t *testing.T, ch <-chan NotificationMessage) NotificationMessage {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("no se envió ninguna notificación")
		return NotificationMessage{}
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestPushNotifyCustomerGuest(t *testing.T) {
	booking := newTestBooking()
	booking.GuestDeviceID = "guest-1"

	devices := &fakeDeviceRepo{byGuest: map[string][]domain.UserDevice{
		"guest-1": {{FCMToken: "token-1"}},
	}}
	sender := &fakeSender{ch: make(chan NotificationMessage, 1)}
	push := NewPushService(devices, nil, sender, &fakeDeliveries{seen: map[string]bool{}})

	push.NotifyCustomer(context.Background(), booking, PushEvent{
		Type: domain.InboxBookingConfirmed, Title: "Reserva confirmada", Body: "Listo",
	})

	msg := receive(t, sender.ch)
	if len(msg.Tokens) != 1 || msg.Tokens[0] != "token-1" {
		t.Fatalf("tokens = %v, esperaba [token-1]", msg.Tokens)
	}
	if msg.Audience != domain.DeviceAppClient {
		t.Errorf("audience = %q, esperaba client", msg.Audience)
	}
	if got := msg.Data["type"]; got != domain.InboxBookingConfirmed {
		t.Errorf("data.type = %q", got)
	}
	if got := msg.Data["booking_code"]; got != "abc123" {
		t.Errorf("data.booking_code = %q", got)
	}
}

func TestPushNotifyAdminsDeduplicatesTokens(t *testing.T) {
	booking := newTestBooking()
	centerID := booking.SportCenterID.Hex()

	devices := &fakeDeviceRepo{byCenter: map[string][]domain.UserDevice{
		centerID: {{FCMToken: "token-1"}, {FCMToken: "token-1"}, {FCMToken: "token-2"}},
	}}
	sender := &fakeSender{ch: make(chan NotificationMessage, 1)}
	push := NewPushService(devices, nil, sender, &fakeDeliveries{seen: map[string]bool{}})

	push.NotifyAdmins(context.Background(), booking, PushEvent{Type: "confirmation", Title: "Nueva reserva", Body: "20:00"})

	msg := receive(t, sender.ch)
	if len(msg.Tokens) != 2 {
		t.Fatalf("tokens = %v, esperaba 2 sin duplicados", msg.Tokens)
	}
	if msg.Audience != domain.DeviceAppAdmin {
		t.Errorf("audience = %q, esperaba admin", msg.Audience)
	}
}

func TestPushDedupeStopsRepeatedSends(t *testing.T) {
	booking := newTestBooking()
	booking.UserID = "user-1"

	devices := &fakeDeviceRepo{byUser: map[string][]domain.UserDevice{
		"user-1": {{FCMToken: "token-1"}},
	}}
	sender := &fakeSender{ch: make(chan NotificationMessage, 2)}
	push := NewPushService(devices, nil, sender, &fakeDeliveries{seen: map[string]bool{}})

	ev := PushEvent{Type: domain.InboxBookingConfirmed, Title: "Reserva confirmada", Body: "Listo"}
	push.NotifyCustomer(context.Background(), booking, ev)
	receive(t, sender.ch)

	// El webhook se reintenta: no debe volver a enviarse.
	push.NotifyCustomer(context.Background(), booking, ev)
	select {
	case msg := <-sender.ch:
		t.Fatalf("se envió dos veces: %+v", msg)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestPushResendsAfterReschedule(t *testing.T) {
	booking := newTestBooking()
	booking.UserID = "user-1"

	devices := &fakeDeviceRepo{byUser: map[string][]domain.UserDevice{
		"user-1": {{FCMToken: "token-1"}},
	}}
	sender := &fakeSender{ch: make(chan NotificationMessage, 2)}
	push := NewPushService(devices, nil, sender, &fakeDeliveries{seen: map[string]bool{}})

	ev := PushEvent{Type: domain.InboxBookingReminder, Title: "Recordatorio", Body: "20:00"}
	push.NotifyCustomer(context.Background(), booking, ev)
	receive(t, sender.ch)

	// La reserva cambia de hora: el recordatorio del horario nuevo sí se envía.
	booking.Hour = 21
	push.NotifyCustomer(context.Background(), booking, ev)
	receive(t, sender.ch)

	// El mismo horario nuevo no se repite.
	push.NotifyCustomer(context.Background(), booking, ev)
	select {
	case msg := <-sender.ch:
		t.Fatalf("se envió dos veces el mismo horario: %+v", msg)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestPushConfirmationDedupeIgnoresSlot(t *testing.T) {
	booking := newTestBooking()
	booking.UserID = "user-1"

	devices := &fakeDeviceRepo{byUser: map[string][]domain.UserDevice{
		"user-1": {{FCMToken: "token-1"}},
	}}
	sender := &fakeSender{ch: make(chan NotificationMessage, 2)}
	push := NewPushService(devices, nil, sender, &fakeDeliveries{seen: map[string]bool{}})

	ev := PushEvent{Type: domain.InboxBookingConfirmed, Title: "Reserva confirmada", Body: "Listo"}
	push.NotifyCustomer(context.Background(), booking, ev)
	receive(t, sender.ch)

	// El reconciliador puede volver a confirmar tras un cambio de hora: la
	// confirmación no debe repetirse (solo el recordatorio se ata al horario).
	booking.Hour = 21
	push.NotifyCustomer(context.Background(), booking, ev)
	select {
	case msg := <-sender.ch:
		t.Fatalf("la confirmación se repitió tras cambiar de hora: %+v", msg)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestPushPrunesInvalidTokens(t *testing.T) {
	booking := newTestBooking()
	booking.UserID = "user-1"

	devices := &fakeDeviceRepo{byUser: map[string][]domain.UserDevice{
		"user-1": {{FCMToken: "token-1"}, {FCMToken: "token-2"}},
	}}
	sender := &fakeSender{
		ch:     make(chan NotificationMessage, 1),
		result: PushResult{SuccessCount: 1, FailureCount: 1, InvalidTokens: []string{"token-2"}},
	}
	push := NewPushService(devices, nil, sender, &fakeDeliveries{seen: map[string]bool{}})

	push.NotifyCustomer(context.Background(), booking, PushEvent{
		Type: domain.InboxBookingCancelled, Title: "Reserva cancelada", Body: "Cancelada",
	})
	receive(t, sender.ch)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		devices.mu.Lock()
		deleted := len(devices.deleted)
		devices.mu.Unlock()
		if deleted == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no se podó el token inválido")
}
