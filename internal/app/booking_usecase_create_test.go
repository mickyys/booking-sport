package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// mockBookingRepoForCreate embeds the full BookingRepository mock but records
// the bookings passed to Create.
type mockBookingRepoForCreate struct {
	*mockBookingRepoForHold
	Created []*domain.Booking
}

func (m *mockBookingRepoForCreate) Create(ctx context.Context, booking *domain.Booking) error {
	m.Created = append(m.Created, booking)
	return nil
}

var _ BookingRepository = (*mockBookingRepoForCreate)(nil)

func availableSlot(hour int, price float64) domain.CourtSchedule {
	return domain.CourtSchedule{
		Hour:            hour,
		Minutes:         0,
		Price:           price,
		Status:          "available",
		PaymentRequired: false,
	}
}

func newCreateBookingUseCase(court *domain.Court, bookingRepo *mockBookingRepoForCreate, holdRepo *mockSlotHoldRepo) *BookingUseCase {
	courtRepo := &mockCourtRepoForRecurring{
		FindByIDFn: func(ctx context.Context, id primitive.ObjectID) (*domain.Court, error) {
			return court, nil
		},
	}
	return &BookingUseCase{
		repo:                     bookingRepo,
		holdRepo:                 holdRepo,
		courtRepo:                courtRepo,
		centerRepo:               &mockCenterRepoForRecurring{},
		recurringReservationRepo: &mockRecurringReservationRepo{},
	}
}

func assertErrorContains(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("expected error containing %q, got %q", substr, err.Error())
	}
}

func TestCreate_SetsPresentialPaymentMethod(t *testing.T) {
	courtID := newObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: newObjectID(),
		Name:          "Cancha 1",
		Schedule:      []domain.CourtSchedule{availableSlot(10, 30000)},
	}

	bookingRepo := &mockBookingRepoForCreate{mockBookingRepoForHold: &mockBookingRepoForHold{}}
	uc := newCreateBookingUseCase(court, bookingRepo, &mockSlotHoldRepo{})

	booking := &domain.Booking{
		CourtID: courtID,
		Hour:    10,
		Date:    time.Now().AddDate(0, 0, 2),
		GuestDetails: &domain.GuestDetails{
			Name:  "Juan Perez",
			Phone: "+56912345678",
		},
	}

	err := uc.Create(context.Background(), booking)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(bookingRepo.Created) != 1 {
		t.Fatalf("expected 1 created booking, got %d", len(bookingRepo.Created))
	}

	created := bookingRepo.Created[0]
	if created.PaymentMethod != "presential" {
		t.Errorf("expected PaymentMethod=presential, got %q", created.PaymentMethod)
	}
	if created.Status != domain.BookingStatusConfirmed {
		t.Errorf("expected status confirmed, got %q", created.Status)
	}
	if created.Price != 30000 {
		t.Errorf("expected price 30000, got %v", created.Price)
	}
	if created.FinalPrice != 30000 {
		t.Errorf("expected final price 30000, got %v", created.FinalPrice)
	}
	if created.BookingCode == "" {
		t.Error("expected booking code to be generated")
	}
	if created.SportCenterName != "Centro Deportivo Test" {
		t.Errorf("expected center name, got %q", created.SportCenterName)
	}
	if created.CourtName != "Cancha 1" {
		t.Errorf("expected court name, got %q", created.CourtName)
	}
	if created.CustomerName != "Juan Perez" {
		t.Errorf("expected customer name from guest details, got %q", created.CustomerName)
	}
	if created.CustomerPhone != "+56912345678" {
		t.Errorf("expected customer phone from guest details, got %q", created.CustomerPhone)
	}
}

func TestCreate_RejectsPaymentRequiredSlot(t *testing.T) {
	slot := availableSlot(10, 30000)
	slot.PaymentRequired = true
	courtID := newObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: newObjectID(),
		Name:          "Cancha 1",
		Schedule:      []domain.CourtSchedule{slot},
	}

	bookingRepo := &mockBookingRepoForCreate{mockBookingRepoForHold: &mockBookingRepoForHold{}}
	uc := newCreateBookingUseCase(court, bookingRepo, &mockSlotHoldRepo{})

	err := uc.Create(context.Background(), &domain.Booking{
		CourtID: courtID,
		Hour:    10,
		Date:    time.Now().AddDate(0, 0, 2),
	})
	assertErrorContains(t, err, "payment required")
	if len(bookingRepo.Created) != 0 {
		t.Errorf("expected no booking to be created, got %d", len(bookingRepo.Created))
	}
}

func TestCreate_RejectsUnavailableSlot(t *testing.T) {
	slot := availableSlot(10, 30000)
	slot.Status = "booked"
	courtID := newObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: newObjectID(),
		Name:          "Cancha 1",
		Schedule:      []domain.CourtSchedule{slot},
	}

	bookingRepo := &mockBookingRepoForCreate{mockBookingRepoForHold: &mockBookingRepoForHold{}}
	uc := newCreateBookingUseCase(court, bookingRepo, &mockSlotHoldRepo{})

	err := uc.Create(context.Background(), &domain.Booking{
		CourtID: courtID,
		Hour:    10,
		Date:    time.Now().AddDate(0, 0, 2),
	})
	assertErrorContains(t, err, "not available")
	if len(bookingRepo.Created) != 0 {
		t.Errorf("expected no booking to be created, got %d", len(bookingRepo.Created))
	}
}

func TestCreate_RejectsHourNotFound(t *testing.T) {
	courtID := newObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: newObjectID(),
		Name:          "Cancha 1",
		Schedule:      []domain.CourtSchedule{availableSlot(10, 30000)},
	}

	bookingRepo := &mockBookingRepoForCreate{mockBookingRepoForHold: &mockBookingRepoForHold{}}
	uc := newCreateBookingUseCase(court, bookingRepo, &mockSlotHoldRepo{})

	err := uc.Create(context.Background(), &domain.Booking{
		CourtID: courtID,
		Hour:    12,
		Date:    time.Now().AddDate(0, 0, 2),
	})
	assertErrorContains(t, err, "hour 12 not found")
	if len(bookingRepo.Created) != 0 {
		t.Errorf("expected no booking to be created, got %d", len(bookingRepo.Created))
	}
}

func TestCreate_RejectsConfirmedConflict(t *testing.T) {
	courtID := newObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: newObjectID(),
		Name:          "Cancha 1",
		Schedule:      []domain.CourtSchedule{availableSlot(10, 30000)},
	}

	bookingRepo := &mockBookingRepoForCreate{
		mockBookingRepoForHold: &mockBookingRepoForHold{
			FindConfirmedBySlotFn: func(ctx context.Context, cID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
				return &domain.Booking{ID: newObjectID(), CourtID: cID, Hour: hour}, nil
			},
		},
	}
	uc := newCreateBookingUseCase(court, bookingRepo, &mockSlotHoldRepo{})

	err := uc.Create(context.Background(), &domain.Booking{
		CourtID: courtID,
		Hour:    10,
		Date:    time.Now().AddDate(0, 0, 2),
	})
	assertErrorContains(t, err, "ya existe una reserva confirmada")
	if len(bookingRepo.Created) != 0 {
		t.Errorf("expected no booking to be created, got %d", len(bookingRepo.Created))
	}
}

func TestCreateInternalBooking_SetsPresentialPaymentMethod(t *testing.T) {
	courtID := newObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: newObjectID(),
		Name:          "Cancha 1",
		Schedule:      []domain.CourtSchedule{availableSlot(10, 30000)},
	}

	bookingRepo := &mockBookingRepoForCreate{mockBookingRepoForHold: &mockBookingRepoForHold{}}
	uc := newCreateBookingUseCase(court, bookingRepo, &mockSlotHoldRepo{})

	booking := &domain.Booking{
		CourtID: courtID,
		Hour:    10,
		Date:    time.Now().AddDate(0, 0, 2),
	}

	err := uc.CreateInternalBooking(context.Background(), booking, "presential")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(bookingRepo.Created) != 1 {
		t.Fatalf("expected 1 created booking, got %d", len(bookingRepo.Created))
	}

	created := bookingRepo.Created[0]
	if created.PaymentMethod != "presential" {
		t.Errorf("expected PaymentMethod=presential, got %q", created.PaymentMethod)
	}
	if created.Status != domain.BookingStatusConfirmed {
		t.Errorf("expected status confirmed, got %q", created.Status)
	}
	if created.Price != 30000 {
		t.Errorf("expected price 30000, got %v", created.Price)
	}
	if created.BookingCode == "" {
		t.Error("expected booking code to be generated")
	}
}

func TestCreateInternalBooking_UsesProvidedPaymentMethod(t *testing.T) {
	courtID := newObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: newObjectID(),
		Name:          "Cancha 1",
		Schedule:      []domain.CourtSchedule{availableSlot(10, 30000)},
	}

	bookingRepo := &mockBookingRepoForCreate{mockBookingRepoForHold: &mockBookingRepoForHold{}}
	uc := newCreateBookingUseCase(court, bookingRepo, &mockSlotHoldRepo{})

	err := uc.CreateInternalBooking(context.Background(), &domain.Booking{
		CourtID: courtID,
		Hour:    10,
		Date:    time.Now().AddDate(0, 0, 2),
	}, "internal")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(bookingRepo.Created) != 1 {
		t.Fatalf("expected 1 created booking, got %d", len(bookingRepo.Created))
	}
	if got := bookingRepo.Created[0].PaymentMethod; got != "internal" {
		t.Errorf("expected PaymentMethod=internal, got %q", got)
	}
}

func TestCreateInternalBooking_RejectsPastSlot(t *testing.T) {
	courtID := newObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: newObjectID(),
		Name:          "Cancha 1",
		Schedule:      []domain.CourtSchedule{availableSlot(10, 30000)},
	}

	bookingRepo := &mockBookingRepoForCreate{mockBookingRepoForHold: &mockBookingRepoForHold{}}
	uc := newCreateBookingUseCase(court, bookingRepo, &mockSlotHoldRepo{})

	err := uc.CreateInternalBooking(context.Background(), &domain.Booking{
		CourtID: courtID,
		Hour:    10,
		Date:    time.Now().AddDate(0, 0, -1),
	}, "presential")
	assertErrorContains(t, err, "cannot book a past slot")
	if len(bookingRepo.Created) != 0 {
		t.Errorf("expected no booking to be created, got %d", len(bookingRepo.Created))
	}
}

func TestCreateInternalBooking_RejectsConfirmedConflict(t *testing.T) {
	courtID := newObjectID()
	court := &domain.Court{
		ID:            courtID,
		SportCenterID: newObjectID(),
		Name:          "Cancha 1",
		Schedule:      []domain.CourtSchedule{availableSlot(10, 30000)},
	}

	bookingRepo := &mockBookingRepoForCreate{
		mockBookingRepoForHold: &mockBookingRepoForHold{
			FindConfirmedBySlotFn: func(ctx context.Context, cID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
				return &domain.Booking{ID: newObjectID(), CourtID: cID, Hour: hour}, nil
			},
		},
	}
	uc := newCreateBookingUseCase(court, bookingRepo, &mockSlotHoldRepo{})

	err := uc.CreateInternalBooking(context.Background(), &domain.Booking{
		CourtID: courtID,
		Hour:    10,
		Date:    time.Now().AddDate(0, 0, 2),
	}, "presential")
	assertErrorContains(t, err, "ya existe un proceso de reserva o reserva confirmada")
	if len(bookingRepo.Created) != 0 {
		t.Errorf("expected no booking to be created, got %d", len(bookingRepo.Created))
	}
}
