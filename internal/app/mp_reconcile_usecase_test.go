package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type fakePaymentEmailStore struct {
	PaymentReconcileStore
	mu       sync.Mutex
	claimed  bool
	released int32
}

func (f *fakePaymentEmailStore) ClaimConfirmationEmail(ctx context.Context, id primitive.ObjectID, at time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimed {
		return false, nil
	}
	f.claimed = true
	return true, nil
}

func (f *fakePaymentEmailStore) ReleaseConfirmationEmail(ctx context.Context, id primitive.ObjectID) error {
	atomic.AddInt32(&f.released, 1)
	f.mu.Lock()
	f.claimed = false
	f.mu.Unlock()
	return nil
}

type fakeMailer struct {
	Mailer
	sent int32
	err  error
	done chan struct{}
}

func (m *fakeMailer) SendBookingConfirmation(ctx context.Context, b *domain.Booking, ch, rp int, paid, pending float64) error {
	atomic.AddInt32(&m.sent, 1)
	defer func() { m.done <- struct{}{} }()
	return m.err
}

func newEmailTestBooking() *domain.Booking {
	return &domain.Booking{
		ID:           primitive.NewObjectID(),
		BookingCode:  "abc12345",
		GuestDetails: &domain.GuestDetails{Email: "cliente@example.com"},
	}
}

func TestSendConfirmationEmailOnce_SoloUnEnvio(t *testing.T) {
	store := &fakePaymentEmailStore{}
	mailer := &fakeMailer{done: make(chan struct{}, 10)}
	uc := &BookingUseCase{mailer: mailer}
	center := &domain.SportCenter{}

	var wg sync.WaitGroup
	var winners int32
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if uc.sendConfirmationEmailOnce(context.Background(), store, newEmailTestBookingShared, center) {
				atomic.AddInt32(&winners, 1)
			}
		}()
	}
	wg.Wait()
	<-mailer.done

	if winners != 1 {
		t.Fatalf("esperaba 1 ganador, hubo %d", winners)
	}
	if got := atomic.LoadInt32(&mailer.sent); got != 1 {
		t.Fatalf("esperaba 1 correo, se enviaron %d", got)
	}
}

var newEmailTestBookingShared = newEmailTestBooking()

func TestSendConfirmationEmailOnce_FalloLiberaMarca(t *testing.T) {
	store := &fakePaymentEmailStore{}
	mailer := &fakeMailer{done: make(chan struct{}, 1), err: errors.New("mailgun down")}
	uc := &BookingUseCase{mailer: mailer}

	if !uc.sendConfirmationEmailOnce(context.Background(), store, newEmailTestBooking(), &domain.SportCenter{}) {
		t.Fatal("debía reclamar el envío")
	}
	<-mailer.done
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt32(&store.released) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt32(&store.released) != 1 {
		t.Fatal("tras fallar el envío debía liberarse la marca para reintentar")
	}
}

func TestSendConfirmationEmailOnce_SinDestinatario(t *testing.T) {
	store := &fakePaymentEmailStore{}
	uc := &BookingUseCase{mailer: &fakeMailer{done: make(chan struct{}, 1)}}
	b := newEmailTestBooking()
	b.GuestDetails = nil
	if uc.sendConfirmationEmailOnce(context.Background(), store, b, &domain.SportCenter{}) {
		t.Fatal("sin correo no debe reclamar el envío")
	}
	if store.claimed {
		t.Fatal("no debe marcarse como enviado")
	}
}
