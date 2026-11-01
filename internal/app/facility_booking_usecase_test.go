package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type fakeFacilitySpaceRepo struct {
	SpaceRepository
	space *domain.Space
}

func (f *fakeFacilitySpaceRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Space, error) {
	if f.space == nil || f.space.ID != id {
		return nil, domain.ErrNotFound
	}
	return f.space, nil
}

type fakeFacilityCenterRepo struct {
	SportCenterRepository
	center *domain.SportCenter
}

func (f *fakeFacilityCenterRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.SportCenter, error) {
	return f.center, nil
}

type fakeFacilityBookingRepo struct {
	FacilityBookingRepository
	docs []*domain.FacilityBooking
}

func (f *fakeFacilityBookingRepo) ExpireStale(ctx context.Context, spaceID primitive.ObjectID, now time.Time) error {
	return nil
}

func (f *fakeFacilityBookingRepo) CreateMany(ctx context.Context, bookings []*domain.FacilityBooking) error {
	var added []*domain.FacilityBooking
	for _, b := range bookings {
		for _, e := range append(append([]*domain.FacilityBooking{}, f.docs...), added...) {
			if e.Active && e.SpaceID == b.SpaceID && e.LocalDate == b.LocalDate {
				return domain.ErrBookingSlotTaken
			}
		}
		added = append(added, b)
	}
	f.docs = append(f.docs, added...)
	return nil
}

func (f *fakeFacilityBookingRepo) CancelByCode(ctx context.Context, code, by string, status domain.BookingStatus) error {
	return nil
}

func newFacilityBookingFixture(mode string, category domain.SpaceCategory, capacity int) (*FacilityBookingUseCase, *domain.Space, *fakeFacilityBookingRepo) {
	space := &domain.Space{
		ID:          primitive.NewObjectID(),
		Category:    category,
		Name:        "Piscina",
		Enabled:     true,
		Capacity:    capacity,
		PricePerDay: 10000,
	}
	center := &domain.SportCenter{ID: primitive.NewObjectID(), Name: "Centro", PaymentMode: mode}
	space.SportCenterID = center.ID
	repo := &fakeFacilityBookingRepo{}
	uc := NewFacilityBookingUseCase(repo, &fakeFacilitySpaceRepo{space: space}, &fakeFacilityCenterRepo{center: center}, nil, nil)
	return uc, space, repo
}

func futureDate(days int) string {
	return time.Now().In(domain.GetSantiagoLocation()).AddDate(0, 0, days).Format("2006-01-02")
}

func validInput(space *domain.Space, guests int, dates ...string) CreateFacilityBookingInput {
	return CreateFacilityBookingInput{
		SpaceID:       space.ID,
		Dates:         dates,
		Guests:        guests,
		CustomerName:  "Ana",
		CustomerEmail: "ana@example.com",
		CustomerPhone: "+56912345678",
	}
}

func TestFacilityBookingCapacityIsInformational(t *testing.T) {
	ctx := context.Background()

	uc, space, _ := newFacilityBookingFixture(PaymentModePayAtCenter, domain.SpaceCategoryPiscina, 10)
	res, err := uc.Create(ctx, validInput(space, 25, futureDate(2)))
	if err != nil {
		t.Fatalf("superar el máximo informativo no debe bloquear: %v", err)
	}
	if res.Status != domain.BookingStatusConfirmed {
		t.Fatalf("pago en el centro debe confirmar, got %s", res.Status)
	}

	uc, space, _ = newFacilityBookingFixture(PaymentModePayAtCenter, domain.SpaceCategorySede, 0)
	if _, err := uc.Create(ctx, validInput(space, 0, futureDate(2))); err != nil {
		t.Fatalf("espacio sin máximo y sin personas no debe bloquear: %v", err)
	}
	if _, err := uc.Create(ctx, validInput(space, -1, futureDate(3))); err == nil {
		t.Fatal("cantidad negativa de personas debe rechazarse")
	}
}

func TestFacilityBookingOnePerDay(t *testing.T) {
	uc, space, _ := newFacilityBookingFixture(PaymentModePayAtCenter, domain.SpaceCategoryPiscina, 10)
	ctx := context.Background()
	day := futureDate(3)

	if _, err := uc.Create(ctx, validInput(space, 2, day)); err != nil {
		t.Fatalf("primera reserva: %v", err)
	}
	_, err := uc.Create(ctx, validInput(space, 2, futureDate(4), day))
	var appErr *domain.AppError
	if !errors.As(err, &appErr) || appErr.StatusCode != 409 {
		t.Fatalf("día ya reservado debe dar 409, got %v", err)
	}
}

func TestFacilityBookingRejectsPastDate(t *testing.T) {
	uc, space, _ := newFacilityBookingFixture(PaymentModePayAtCenter, domain.SpaceCategoryPiscina, 10)
	if _, err := uc.Create(context.Background(), validInput(space, 2, futureDate(-1))); err == nil {
		t.Fatal("no se puede reservar un día pasado")
	}
}

func TestFacilityBookingPaymentModes(t *testing.T) {
	ctx := context.Background()

	uc, space, _ := newFacilityBookingFixture(PaymentModePayOnline, domain.SpaceCategoryPiscina, 10)
	in := validInput(space, 2, futureDate(2))
	in.PaymentMethod = FacilityPaymentVenue
	if _, err := uc.Create(ctx, in); err == nil {
		t.Fatal("pay_online no admite pago en el centro")
	}
	in.PaymentMethod = ""
	if _, err := uc.Create(ctx, in); err == nil {
		t.Fatal("pay_online sin Mercado Pago conectado debe fallar")
	}

	uc, space, _ = newFacilityBookingFixture(PaymentModePayAtCenter, domain.SpaceCategoryPiscina, 10)
	in = validInput(space, 2, futureDate(2))
	in.PaymentMethod = FacilityPaymentOnline
	if _, err := uc.Create(ctx, in); err == nil {
		t.Fatal("pay_at_center no admite pago online")
	}
}

func TestFacilityBookingQuinchoUsesAllDayPrice(t *testing.T) {
	uc, space, _ := newFacilityBookingFixture(PaymentModePayAtCenter, domain.SpaceCategoryQuincho, 20)
	space.PricePerDay = 0
	space.PriceAllDay = 55000

	res, err := uc.Create(context.Background(), validInput(space, 8, futureDate(5)))
	if err != nil {
		t.Fatalf("un quincho se reserva por día completo: %v", err)
	}
	if res.Total != 55000 {
		t.Fatalf("total = %v, want 55000", res.Total)
	}
}

type fakeFacilityMailer struct {
	sent []FacilityConfirmationEmail
	err  error
}

func (f *fakeFacilityMailer) SendFacilityBookingConfirmation(_ context.Context, d FacilityConfirmationEmail) error {
	f.sent = append(f.sent, d)
	return f.err
}

func TestFacilityBookingVenueSendsOneConfirmationEmail(t *testing.T) {
	uc, space, _ := newFacilityBookingFixture(PaymentModePayAtCenter, domain.SpaceCategoryPiscina, 10)
	m := &fakeFacilityMailer{}
	uc.SetMailer(m)
	uc.async = func(f func()) { f() }

	res, err := uc.Create(context.Background(), validInput(space, 3, futureDate(2), futureDate(3)))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.sent) != 1 {
		t.Fatalf("debe enviarse un correo por compra, got %d", len(m.sent))
	}
	got := m.sent[0]
	if got.BookingCode != res.BookingCode || len(got.Dates) != 2 || got.Total != 20000 || got.Guests != 3 {
		t.Errorf("datos inesperados: %+v", got)
	}
}

func TestFacilityBookingOnlinePendingDoesNotSendEmail(t *testing.T) {
	uc, space, _ := newFacilityBookingFixture(PaymentModePayOnline, domain.SpaceCategoryPiscina, 10)
	m := &fakeFacilityMailer{}
	uc.SetMailer(m)
	uc.async = func(f func()) { f() }

	// sin cliente de Mercado Pago la preferencia falla, pero nunca debe haber correo
	_, _ = uc.Create(context.Background(), validInput(space, 2, futureDate(2)))
	if len(m.sent) != 0 {
		t.Fatalf("una reserva pendiente de pago no envía correo, got %d", len(m.sent))
	}
}

func TestFacilityBookingMailFailureDoesNotFailBooking(t *testing.T) {
	uc, space, _ := newFacilityBookingFixture(PaymentModePayAtCenter, domain.SpaceCategoryQuincho, 10)
	uc.SetMailer(&fakeFacilityMailer{err: errors.New("boom")})
	uc.async = func(f func()) { f() }
	if _, err := uc.Create(context.Background(), validInput(space, 2, futureDate(2))); err != nil {
		t.Fatalf("el fallo del correo no debe romper la reserva: %v", err)
	}
}
