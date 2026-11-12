package app

import (
	"context"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
)

func newFacilityCourt(category domain.SpaceCategory, pricePerDay float64) *domain.Court {
	return &domain.Court{
		ID:            newObjectID(),
		SportCenterID: newObjectID(),
		Name:          "Piscina Techada",
		Category:      category,
		PricePerDay:   pricePerDay,
	}
}

func TestCreate_FacilityBooksFullDayAtPricePerDay(t *testing.T) {
	court := newFacilityCourt(domain.SpaceCategoryPiscina, 80000)
	bookingRepo := &mockBookingRepoForCreate{mockBookingRepoForHold: &mockBookingRepoForHold{}}
	uc := newCreateBookingUseCase(court, bookingRepo, &mockSlotHoldRepo{})

	booking := &domain.Booking{
		CourtID: court.ID,
		Hour:    15, // el cliente no decide la hora: una facility es de día completo
		Date:    time.Now().AddDate(0, 0, 3),
		Guests:  12,
		GuestDetails: &domain.GuestDetails{
			Name:  "Ana Soto",
			Phone: "+56911112222",
		},
	}
	if err := uc.Create(context.Background(), booking); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bookingRepo.Created) != 1 {
		t.Fatalf("expected 1 created booking, got %d", len(bookingRepo.Created))
	}
	got := bookingRepo.Created[0]
	if got.Hour != 0 || got.Minutes != 0 || got.DurationMinutes != facilityDayMinutes {
		t.Errorf("expected day block 00:00/1440, got %02d:%02d/%d", got.Hour, got.Minutes, got.DurationMinutes)
	}
	if got.Price != 80000 || got.FinalPrice != 80000 {
		t.Errorf("expected price 80000, got price=%v final=%v", got.Price, got.FinalPrice)
	}
	if got.SpaceCategory != domain.SpaceCategoryPiscina {
		t.Errorf("expected space category piscina, got %q", got.SpaceCategory)
	}
	if got.Guests != 12 {
		t.Errorf("expected 12 guests, got %d", got.Guests)
	}
}

func TestCreate_FacilityAllowsSameDayBooking(t *testing.T) {
	court := newFacilityCourt(domain.SpaceCategorySede, 50000)
	bookingRepo := &mockBookingRepoForCreate{mockBookingRepoForHold: &mockBookingRepoForHold{}}
	uc := newCreateBookingUseCase(court, bookingRepo, &mockSlotHoldRepo{})

	booking := &domain.Booking{
		CourtID:      court.ID,
		Date:         domain.SantiagoDateStart(time.Now()),
		GuestDetails: &domain.GuestDetails{Name: "Pedro", Phone: "+56933334444"},
	}
	if err := uc.Create(context.Background(), booking); err != nil {
		t.Fatalf("a facility should be bookable during its own day: %v", err)
	}
	if bookingRepo.Created[0].Guests != 1 {
		t.Errorf("expected default of 1 guest, got %d", bookingRepo.Created[0].Guests)
	}
}

func TestCreate_FacilityRejectsPastDay(t *testing.T) {
	court := newFacilityCourt(domain.SpaceCategoryPiscina, 50000)
	bookingRepo := &mockBookingRepoForCreate{mockBookingRepoForHold: &mockBookingRepoForHold{}}
	uc := newCreateBookingUseCase(court, bookingRepo, &mockSlotHoldRepo{})

	booking := &domain.Booking{
		CourtID:      court.ID,
		Date:         time.Now().AddDate(0, 0, -2),
		GuestDetails: &domain.GuestDetails{Name: "Pedro", Phone: "+56933334444"},
	}
	assertErrorContains(t, uc.Create(context.Background(), booking), "past slot")
}

func TestCreateInternalBooking_FacilityBlocksFullDay(t *testing.T) {
	court := newFacilityCourt(domain.SpaceCategoryPiscina, 80000)
	bookingRepo := &mockBookingRepoForCreate{mockBookingRepoForHold: &mockBookingRepoForHold{}}
	uc := newCreateBookingUseCase(court, bookingRepo, &mockSlotHoldRepo{})

	booking := &domain.Booking{
		CourtID: court.ID,
		Hour:    9,
		Date:    time.Now().AddDate(0, 0, 5),
	}
	if err := uc.CreateInternalBooking(context.Background(), booking, "internal"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := bookingRepo.Created[0]
	if got.Hour != 0 || got.DurationMinutes != facilityDayMinutes {
		t.Errorf("expected full-day block, got hour=%d duration=%d", got.Hour, got.DurationMinutes)
	}
	if got.Status != domain.BookingStatusConfirmed {
		t.Errorf("expected confirmed, got %q", got.Status)
	}
}

func TestBookingFacilityMappingRoundTrip(t *testing.T) {
	loc := domain.GetSantiagoLocation()
	exp := time.Now().Add(15 * time.Minute)
	fb := &domain.FacilityBooking{
		ID:               newObjectID(),
		BookingCode:      "FAC-ABC123",
		SpaceID:          newObjectID(),
		SportCenterID:    newObjectID(),
		SpaceName:        "Piscina",
		SpaceType:        domain.FacilityTypePiscina,
		SportCenterName:  "Centro",
		LocalDate:        time.Now().In(loc).AddDate(0, 0, 4).Format("2006-01-02"),
		Guests:           6,
		CustomerName:     "Ana",
		CustomerEmail:    "ana@example.com",
		CustomerPhone:    "+569111",
		Status:           domain.BookingStatusPending,
		PaymentMethod:    FacilityPaymentOnline,
		Price:            90000,
		PendingAmount:    90000,
		IsPartialPayment: true,
		ExpiresAt:        &exp,
		CommissionRate:   5,
		CommissionAmount: 2250,
	}
	b, err := facilityToBooking(fb)
	if err != nil {
		t.Fatal(err)
	}
	if b.GroupCode != "FAC-ABC123" || b.BookingCode == b.GroupCode {
		t.Errorf("each day needs its own unique code, got group=%q code=%q", b.GroupCode, b.BookingCode)
	}
	if b.PaymentMethod != "mercadopago" || b.SpaceCategory != domain.SpaceCategoryPiscina {
		t.Errorf("unexpected mapping: %+v", b)
	}
	if b.Pricing == nil || b.Pricing.Commission == nil || b.Pricing.Commission.Amount != 2250 {
		t.Errorf("commission snapshot lost: %+v", b.Pricing)
	}

	back := bookingToFacility(b)
	if back.BookingCode != "FAC-ABC123" || back.LocalDate != fb.LocalDate || back.Guests != 6 ||
		back.CustomerEmail != "ana@example.com" || back.PaymentMethod != FacilityPaymentOnline || !back.Active {
		t.Errorf("round trip mismatch: %+v", back)
	}
}
