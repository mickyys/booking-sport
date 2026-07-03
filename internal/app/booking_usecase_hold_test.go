package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// ---------- BookingRepository mock for ClaimOrRenewSlot ----------

type mockBookingRepoForHold struct {
	FindConfirmedBySlotFn func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error)
	FindPendingBySlotFn   func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error)
	UpdateLockExpiresAtFn func(ctx context.Context, id primitive.ObjectID, expiresAt time.Time) error
	MarkExpiredFn         func(ctx context.Context, id primitive.ObjectID) error
	FindByIDFn            func(ctx context.Context, id primitive.ObjectID) (*domain.Booking, error)

	FindConfirmedBySlotCalls []findConfirmedBySlotCall
	FindPendingBySlotCalls   []findPendingBySlotCall
	UpdateLockExpiresAtCalls int
	MarkExpiredCalls         []primitive.ObjectID
	FindByIDCalls            []primitive.ObjectID
}

type findConfirmedBySlotCall struct {
	CourtID primitive.ObjectID
	Date    time.Time
	Hour    int
}

type findPendingBySlotCall struct {
	CourtID primitive.ObjectID
	Date    time.Time
	Hour    int
}

func (m *mockBookingRepoForHold) FindActiveSeriesByCourtHour(ctx context.Context, courtID primitive.ObjectID, hour int) ([]domain.Booking, error) {
	return nil, nil
}

func (m *mockBookingRepoForHold) FindConfirmedBySlot(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
	m.FindConfirmedBySlotCalls = append(m.FindConfirmedBySlotCalls, findConfirmedBySlotCall{courtID, date, hour})
	if m.FindConfirmedBySlotFn != nil {
		return m.FindConfirmedBySlotFn(ctx, courtID, date, hour)
	}
	return nil, nil
}

func (m *mockBookingRepoForHold) FindPendingBySlot(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
	m.FindPendingBySlotCalls = append(m.FindPendingBySlotCalls, findPendingBySlotCall{courtID, date, hour})
	if m.FindPendingBySlotFn != nil {
		return m.FindPendingBySlotFn(ctx, courtID, date, hour)
	}
	return nil, nil
}

func (m *mockBookingRepoForHold) UpdateLockExpiresAt(ctx context.Context, id primitive.ObjectID, expiresAt time.Time) error {
	m.UpdateLockExpiresAtCalls++
	if m.UpdateLockExpiresAtFn != nil {
		return m.UpdateLockExpiresAtFn(ctx, id, expiresAt)
	}
	return nil
}

func (m *mockBookingRepoForHold) MarkExpired(ctx context.Context, id primitive.ObjectID) error {
	m.MarkExpiredCalls = append(m.MarkExpiredCalls, id)
	if m.MarkExpiredFn != nil {
		return m.MarkExpiredFn(ctx, id)
	}
	return nil
}

func (m *mockBookingRepoForHold) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Booking, error) {
	m.FindByIDCalls = append(m.FindByIDCalls, id)
	if m.FindByIDFn != nil {
		return m.FindByIDFn(ctx, id)
	}
	return nil, nil
}

// ---- Stubs for remaining BookingRepository methods ----

func (m *mockBookingRepoForHold) Create(ctx context.Context, booking *domain.Booking) error {
	return nil
}
func (m *mockBookingRepoForHold) Update(ctx context.Context, booking *domain.Booking) error {
	return nil
}
func (m *mockBookingRepoForHold) FindByPreferenceID(ctx context.Context, preferenceID string) (*domain.Booking, error) {
	return nil, nil
}
func (m *mockBookingRepoForHold) FindByFintocPaymentID(ctx context.Context, fintocPaymentID string) (*domain.Booking, error) {
	return nil, nil
}
func (m *mockBookingRepoForHold) FindByFintocPaymentIntentID(ctx context.Context, paymentIntentID string) (*domain.Booking, error) {
	return nil, nil
}
func (m *mockBookingRepoForHold) FindByMPPreferenceID(ctx context.Context, preferenceID string) (*domain.Booking, error) {
	return nil, nil
}
func (m *mockBookingRepoForHold) FindByMPPaymentID(ctx context.Context, paymentID string) (*domain.Booking, error) {
	return nil, nil
}
func (m *mockBookingRepoForHold) FindByBookingCode(ctx context.Context, code string) (*domain.Booking, error) {
	return nil, nil
}
func (m *mockBookingRepoForHold) UpdateStatus(ctx context.Context, id primitive.ObjectID, status domain.BookingStatus) error {
	return nil
}
func (m *mockBookingRepoForHold) ConfirmPayment(ctx context.Context, id primitive.ObjectID, status domain.BookingStatus, paidAmount, pendingAmount float64, paymentInfo *domain.PaymentInfo) error {
	return nil
}
func (m *mockBookingRepoForHold) MarkBalanceAsPaid(ctx context.Context, id primitive.ObjectID, modifiedBy string) error {
	return nil
}
func (m *mockBookingRepoForHold) UndoBalancePayment(ctx context.Context, id primitive.ObjectID, modifiedBy string) error {
	return nil
}
func (m *mockBookingRepoForHold) UpdateCancellation(ctx context.Context, id primitive.ObjectID, status domain.BookingStatus, cancelledBy string, reason string) error {
	return nil
}
func (m *mockBookingRepoForHold) UpdateFintocPaymentIntentID(ctx context.Context, id primitive.ObjectID, paymentIntentID string) error {
	return nil
}
func (m *mockBookingRepoForHold) UpdateMPPaymentID(ctx context.Context, id primitive.ObjectID, mpPaymentID string) error {
	return nil
}
func (m *mockBookingRepoForHold) UpdateFintocPaymentID(ctx context.Context, id primitive.ObjectID, paymentID string) error {
	return nil
}
func (m *mockBookingRepoForHold) AddRefund(ctx context.Context, paymentIntentID string, refund domain.Refund) error {
	return nil
}
func (m *mockBookingRepoForHold) AddRefundByBookingID(ctx context.Context, bookingID primitive.ObjectID, refund domain.Refund) error {
	return nil
}
func (m *mockBookingRepoForHold) FindByCourtAndDate(ctx context.Context, courtID primitive.ObjectID, date time.Time) ([]domain.Booking, error) {
	return nil, nil
}
func (m *mockBookingRepoForHold) FindBySportCenterAndDate(ctx context.Context, centerID primitive.ObjectID, date time.Time) ([]domain.Booking, error) {
	return nil, nil
}
func (m *mockBookingRepoForHold) FindConflictingBooking(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
	return nil, nil
}
func (m *mockBookingRepoForHold) FindConfirmedByCourtAndDate(ctx context.Context, courtID primitive.ObjectID, date time.Time) ([]domain.Booking, error) {
	return nil, nil
}
func (m *mockBookingRepoForHold) FindByUserID(ctx context.Context, userID string) ([]domain.Booking, error) {
	return nil, nil
}
func (m *mockBookingRepoForHold) FindByUserIDPaged(ctx context.Context, userID string, page, limit int, isOld bool) ([]domain.BookingSummary, int64, error) {
	return nil, 0, nil
}
func (m *mockBookingRepoForHold) CountConfirmedByUserID(ctx context.Context, userID string) (int64, error) {
	return 0, nil
}
func (m *mockBookingRepoForHold) FindByUserIDAndStatusPaged(ctx context.Context, userID string, cancelled domain.BookingStatus, page int, limit int) ([]domain.BookingSummary, int64, error) {
	return nil, 0, nil
}
func (m *mockBookingRepoForHold) Delete(ctx context.Context, id primitive.ObjectID) error {
	return nil
}
func (m *mockBookingRepoForHold) DeleteBySeriesID(ctx context.Context, seriesID string) error {
	return nil
}
func (m *mockBookingRepoForHold) GetDashboardData(ctx context.Context, sportCenterIDs []primitive.ObjectID, page, limit int, dateStr, name string, code string, status string) (*domain.AdminDashboardData, error) {
	return nil, nil
}
func (m *mockBookingRepoForHold) GetRecurringSeries(ctx context.Context, centerIDs []primitive.ObjectID, sportCenterID string) ([]domain.RecurringSeries, error) {
	return nil, nil
}
func (m *mockBookingRepoForHold) UpdateHoldID(ctx context.Context, id primitive.ObjectID, holdID primitive.ObjectID) error {
	return nil
}
func (m *mockBookingRepoForHold) ConfirmPaymentWithVersion(ctx context.Context, id primitive.ObjectID, status domain.BookingStatus, paidAmount, pendingAmount float64, currentVersion int, paymentInfo *domain.PaymentInfo) error {
	return nil
}
func (m *mockBookingRepoForHold) GetDB() *mongo.Database {
	return nil
}
func (m *mockBookingRepoForHold) Collection() *mongo.Collection {
	return nil
}

var _ BookingRepository = (*mockBookingRepoForHold)(nil)

// ---------- SlotHoldRepository mock for ClaimOrRenewSlot ----------

type mockSlotHoldRepo struct {
	FindByBookingIDFn           func(ctx context.Context, bookingID primitive.ObjectID) (*domain.SlotHold, error)
	RenewExpirationFn           func(ctx context.Context, holdID primitive.ObjectID, newExpiresAt time.Time) error
	TryClaimSlotFn              func(ctx context.Context, hold *domain.SlotHold) (*domain.SlotHold, error)
	DeleteFn                    func(ctx context.Context, holdID primitive.ObjectID) error
	FindOneAndDeleteIfExpiredFn func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.SlotHold, error)
	FindBySlotFn                func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.SlotHold, error)

	TryClaimSlotCalls              int
	FindOneAndDeleteIfExpiredCalls int
	RenewExpirationCalls           int
	DeleteCalls                    []primitive.ObjectID
	FindBySlotCalls                int
}

func (m *mockSlotHoldRepo) Insert(ctx context.Context, hold *domain.SlotHold) error {
	return nil
}

func (m *mockSlotHoldRepo) FindBySlot(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.SlotHold, error) {
	m.FindBySlotCalls++
	if m.FindBySlotFn != nil {
		return m.FindBySlotFn(ctx, courtID, date, hour)
	}
	return nil, nil
}

func (m *mockSlotHoldRepo) FindByBookingID(ctx context.Context, bookingID primitive.ObjectID) (*domain.SlotHold, error) {
	if m.FindByBookingIDFn != nil {
		return m.FindByBookingIDFn(ctx, bookingID)
	}
	return nil, nil
}

func (m *mockSlotHoldRepo) FindActiveByCourtAndDate(ctx context.Context, courtID primitive.ObjectID, date time.Time) ([]domain.SlotHold, error) {
	return nil, nil
}

func (m *mockSlotHoldRepo) RenewExpiration(ctx context.Context, holdID primitive.ObjectID, newExpiresAt time.Time) error {
	m.RenewExpirationCalls++
	if m.RenewExpirationFn != nil {
		return m.RenewExpirationFn(ctx, holdID, newExpiresAt)
	}
	return nil
}

func (m *mockSlotHoldRepo) DeleteIfExpired(ctx context.Context, holdID primitive.ObjectID, expectedExpiresAt time.Time) (bool, error) {
	return false, nil
}

func (m *mockSlotHoldRepo) Delete(ctx context.Context, holdID primitive.ObjectID) error {
	m.DeleteCalls = append(m.DeleteCalls, holdID)
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, holdID)
	}
	return nil
}

func (m *mockSlotHoldRepo) TryClaimSlot(ctx context.Context, hold *domain.SlotHold) (*domain.SlotHold, error) {
	m.TryClaimSlotCalls++
	if m.TryClaimSlotFn != nil {
		return m.TryClaimSlotFn(ctx, hold)
	}
	return hold, nil
}

func (m *mockSlotHoldRepo) FindOneAndDeleteIfExpired(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.SlotHold, error) {
	m.FindOneAndDeleteIfExpiredCalls++
	if m.FindOneAndDeleteIfExpiredFn != nil {
		return m.FindOneAndDeleteIfExpiredFn(ctx, courtID, date, hour)
	}
	return nil, nil
}

var _ SlotHoldRepository = (*mockSlotHoldRepo)(nil)

// ---------- Helpers ----------

func newObjectID() primitive.ObjectID {
	return primitive.NewObjectID()
}

func chileDate(year int, month time.Month, day int) time.Time {
	loc, _ := time.LoadLocation("America/Santiago")
	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}

func futureLockTime() time.Time {
	return time.Now().Add(1 * time.Hour)
}

func pastLockTime() time.Time {
	return time.Now().Add(-1 * time.Hour)
}

func futureHoldExpiry() time.Time {
	return time.Now().Add(1 * time.Hour)
}

func pastHoldExpiry() time.Time {
	return time.Now().Add(-1 * time.Hour)
}

// ---------- Tests ----------

func TestClaimOrRenewSlot_AlreadyConfirmed(t *testing.T) {
	courtID := newObjectID()
	date := chileDate(2026, 5, 13)
	hour := 10

	confirmedBooking := &domain.Booking{ID: newObjectID()}

	bookingRepo := &mockBookingRepoForHold{
		FindConfirmedBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
			return confirmedBooking, nil
		},
	}
	holdRepo := &mockSlotHoldRepo{}

	uc := &BookingUseCase{repo: bookingRepo, holdRepo: holdRepo}

	hold, booking, err := uc.ClaimOrRenewSlot(context.Background(), courtID, date, hour, "device:abc")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if hold != nil {
		t.Error("expected nil hold")
	}
	if booking != nil {
		t.Error("expected nil booking")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if appErr.StatusCode != 409 {
		t.Errorf("expected status 409, got %d", appErr.StatusCode)
	}
	if appErr.Message != "este horario ya esta reservado" {
		t.Errorf("expected message 'este horario ya esta reservado', got %q", appErr.Message)
	}
}

func TestClaimOrRenewSlot_SameUserRenewsLockWithHold(t *testing.T) {
	courtID := newObjectID()
	date := chileDate(2026, 5, 13)
	hour := 10
	userID := "device:abc"

	futureLock := futureLockTime()
	pendingBooking := &domain.Booking{
		ID:            newObjectID(),
		GuestDeviceID: userID,
		LockExpiresAt: &futureLock,
	}

	existingHold := &domain.SlotHold{
		ID:        newObjectID(),
		CourtID:   courtID,
		Hour:      hour,
		UserID:    userID,
		BookingID: pendingBooking.ID,
	}

	bookingRepo := &mockBookingRepoForHold{
		FindConfirmedBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
			return nil, nil
		},
		FindPendingBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
			return pendingBooking, nil
		},
	}
	holdRepo := &mockSlotHoldRepo{
		FindByBookingIDFn: func(ctx context.Context, bookingID primitive.ObjectID) (*domain.SlotHold, error) {
			return existingHold, nil
		},
	}

	uc := &BookingUseCase{repo: bookingRepo, holdRepo: holdRepo}

	hold, booking, err := uc.ClaimOrRenewSlot(context.Background(), courtID, date, hour, userID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hold == nil {
		t.Fatal("expected non-nil hold")
	}
	if hold.ID != existingHold.ID {
		t.Errorf("expected hold ID %s, got %s", existingHold.ID.Hex(), hold.ID.Hex())
	}
	if booking == nil {
		t.Fatal("expected non-nil booking")
	}
	if booking.ID != pendingBooking.ID {
		t.Errorf("expected booking ID %s, got %s", pendingBooking.ID.Hex(), booking.ID.Hex())
	}
	if bookingRepo.UpdateLockExpiresAtCalls != 1 {
		t.Errorf("expected 1 UpdateLockExpiresAt call, got %d", bookingRepo.UpdateLockExpiresAtCalls)
	}
	if holdRepo.RenewExpirationCalls != 1 {
		t.Errorf("expected 1 RenewExpiration call, got %d", holdRepo.RenewExpirationCalls)
	}
}

func TestClaimOrRenewSlot_SameUserRenewsLockWithoutHold(t *testing.T) {
	courtID := newObjectID()
	date := chileDate(2026, 5, 13)
	hour := 10
	userID := "device:abc"

	futureLock := futureLockTime()
	pendingBooking := &domain.Booking{
		ID:            newObjectID(),
		GuestDeviceID: userID,
		LockExpiresAt: &futureLock,
	}

	bookingRepo := &mockBookingRepoForHold{
		FindConfirmedBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
			return nil, nil
		},
		FindPendingBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
			return pendingBooking, nil
		},
	}
	holdRepo := &mockSlotHoldRepo{
		FindByBookingIDFn: func(ctx context.Context, bookingID primitive.ObjectID) (*domain.SlotHold, error) {
			return nil, nil
		},
		TryClaimSlotFn: func(ctx context.Context, hold *domain.SlotHold) (*domain.SlotHold, error) {
			hold.ID = newObjectID()
			return hold, nil
		},
	}

	uc := &BookingUseCase{repo: bookingRepo, holdRepo: holdRepo}

	hold, booking, err := uc.ClaimOrRenewSlot(context.Background(), courtID, date, hour, userID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hold == nil {
		t.Fatal("expected non-nil hold")
	}
	if hold.UserID != userID {
		t.Errorf("expected hold UserID %s, got %s", userID, hold.UserID)
	}
	if hold.BookingID != pendingBooking.ID {
		t.Errorf("expected hold BookingID %s, got %s", pendingBooking.ID.Hex(), hold.BookingID.Hex())
	}
	if booking == nil {
		t.Fatal("expected non-nil booking")
	}
	if bookingRepo.UpdateLockExpiresAtCalls != 1 {
		t.Errorf("expected 1 UpdateLockExpiresAt call, got %d", bookingRepo.UpdateLockExpiresAtCalls)
	}
	if holdRepo.TryClaimSlotCalls != 1 {
		t.Errorf("expected 1 TryClaimSlot call, got %d", holdRepo.TryClaimSlotCalls)
	}
}

func TestClaimOrRenewSlot_OtherUserHasActiveLock(t *testing.T) {
	courtID := newObjectID()
	date := chileDate(2026, 5, 13)
	hour := 10

	futureLock := futureLockTime()
	otherBooking := &domain.Booking{
		ID:            newObjectID(),
		GuestDeviceID: "device:other",
		UserID:        "auth0|other",
		LockExpiresAt: &futureLock,
	}

	bookingRepo := &mockBookingRepoForHold{
		FindConfirmedBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
			return nil, nil
		},
		FindPendingBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
			return otherBooking, nil
		},
	}
	holdRepo := &mockSlotHoldRepo{}

	uc := &BookingUseCase{repo: bookingRepo, holdRepo: holdRepo}

	hold, booking, err := uc.ClaimOrRenewSlot(context.Background(), courtID, date, hour, "device:abc")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if hold != nil {
		t.Error("expected nil hold")
	}
	if booking != nil {
		t.Error("expected nil booking")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if appErr.StatusCode != 409 {
		t.Errorf("expected status 409, got %d", appErr.StatusCode)
	}
	if !strings.Contains(appErr.Message, "otro usuario esta en proceso de pago") {
		t.Errorf("expected message about other user, got %q", appErr.Message)
	}
}

func TestClaimOrRenewSlot_FreeSlot_ClaimSuccess(t *testing.T) {
	courtID := newObjectID()
	date := chileDate(2026, 5, 13)
	hour := 10
	userID := "device:abc"

	returnedHold := &domain.SlotHold{
		ID:      newObjectID(),
		CourtID: courtID,
		Date:    date,
		Hour:    hour,
		UserID:  userID,
	}

	bookingRepo := &mockBookingRepoForHold{
		FindConfirmedBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
			return nil, nil
		},
		FindPendingBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
			return nil, nil
		},
	}
	holdRepo := &mockSlotHoldRepo{
		TryClaimSlotFn: func(ctx context.Context, hold *domain.SlotHold) (*domain.SlotHold, error) {
			hold.ID = returnedHold.ID
			return hold, nil
		},
	}

	uc := &BookingUseCase{repo: bookingRepo, holdRepo: holdRepo}

	hold, booking, err := uc.ClaimOrRenewSlot(context.Background(), courtID, date, hour, userID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hold == nil {
		t.Fatal("expected non-nil hold")
	}
	if hold.UserID != userID {
		t.Errorf("expected hold UserID %s, got %s", userID, hold.UserID)
	}
	if booking != nil {
		t.Error("expected nil booking for new claim")
	}
	if holdRepo.TryClaimSlotCalls != 1 {
		t.Errorf("expected 1 TryClaimSlot call, got %d", holdRepo.TryClaimSlotCalls)
	}
}

func TestClaimOrRenewSlot_DuplicateHold_SameUser(t *testing.T) {
	courtID := newObjectID()
	date := chileDate(2026, 5, 13)
	hour := 10
	userID := "device:abc"

	existingHold := &domain.SlotHold{
		ID:      newObjectID(),
		CourtID: courtID,
		Date:    date,
		Hour:    hour,
		UserID:  userID,
	}
	pendingBooking := &domain.Booking{
		ID:            existingHold.BookingID,
		GuestDeviceID: userID,
		Status:        domain.BookingStatusPending,
	}

	bookingRepo := &mockBookingRepoForHold{
		FindConfirmedBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
			return nil, nil
		},
		FindPendingBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
			return nil, nil
		},
		FindByIDFn: func(ctx context.Context, id primitive.ObjectID) (*domain.Booking, error) {
			return pendingBooking, nil
		},
	}
	holdRepo := &mockSlotHoldRepo{
		TryClaimSlotFn: func(ctx context.Context, hold *domain.SlotHold) (*domain.SlotHold, error) {
			return nil, errors.New("duplicate hold")
		},
		FindBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.SlotHold, error) {
			return existingHold, nil
		},
	}

	uc := &BookingUseCase{repo: bookingRepo, holdRepo: holdRepo}

	hold, booking, err := uc.ClaimOrRenewSlot(context.Background(), courtID, date, hour, userID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hold == nil {
		t.Fatal("expected non-nil hold")
	}
	if hold.ID != existingHold.ID {
		t.Errorf("expected existing hold ID %s, got %s", existingHold.ID.Hex(), hold.ID.Hex())
	}
	if booking == nil {
		t.Fatal("expected non-nil booking")
	}
	if holdRepo.RenewExpirationCalls != 1 {
		t.Errorf("expected 1 RenewExpiration call, got %d", holdRepo.RenewExpirationCalls)
	}
	if bookingRepo.UpdateLockExpiresAtCalls != 1 {
		t.Errorf("expected 1 UpdateLockExpiresAt call, got %d", bookingRepo.UpdateLockExpiresAtCalls)
	}
}

func TestClaimOrRenewSlot_DuplicateHold_OtherUser(t *testing.T) {
	courtID := newObjectID()
	date := chileDate(2026, 5, 13)
	hour := 10

	existingHold := &domain.SlotHold{
		ID:        newObjectID(),
		CourtID:   courtID,
		Date:      date,
		Hour:      hour,
		UserID:    "device:other",
		ExpiresAt: futureHoldExpiry(),
	}

	bookingRepo := &mockBookingRepoForHold{
		FindConfirmedBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
			return nil, nil
		},
		FindPendingBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
			return nil, nil
		},
	}
	holdRepo := &mockSlotHoldRepo{
		TryClaimSlotFn: func(ctx context.Context, hold *domain.SlotHold) (*domain.SlotHold, error) {
			return nil, errors.New("duplicate hold")
		},
		FindBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.SlotHold, error) {
			return existingHold, nil
		},
	}

	uc := &BookingUseCase{repo: bookingRepo, holdRepo: holdRepo}

	hold, booking, err := uc.ClaimOrRenewSlot(context.Background(), courtID, date, hour, "device:abc")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if hold != nil {
		t.Error("expected nil hold")
	}
	if booking != nil {
		t.Error("expected nil booking")
	}
	appErr, ok := err.(*domain.AppError)
	if !ok {
		t.Fatalf("expected *domain.AppError, got %T", err)
	}
	if appErr.StatusCode != 409 {
		t.Errorf("expected status 409, got %d", appErr.StatusCode)
	}
}

func TestClaimOrRenewSlot_DuplicateHold_ExpiredCleanup(t *testing.T) {
	courtID := newObjectID()
	date := chileDate(2026, 5, 13)
	hour := 10
	userID := "device:abc"

	expiredBookingID := newObjectID()
	expiredHold := &domain.SlotHold{
		ID:        newObjectID(),
		CourtID:   courtID,
		Date:      date,
		Hour:      hour,
		UserID:    "device:other",
		ExpiresAt: pastHoldExpiry(),
		BookingID: expiredBookingID,
	}

	newHoldID := newObjectID()

	holdRepo := &mockSlotHoldRepo{
		TryClaimSlotFn: func(ctx context.Context, hold *domain.SlotHold) (*domain.SlotHold, error) {
			return nil, fmt.Errorf("duplicate hold")
		},
		FindBySlotFn: func(ctx context.Context, cID primitive.ObjectID, d time.Time, h int) (*domain.SlotHold, error) {
			return expiredHold, nil
		},
		FindOneAndDeleteIfExpiredFn: func(ctx context.Context, cID primitive.ObjectID, d time.Time, h int) (*domain.SlotHold, error) {
			return expiredHold, nil
		},
	}

	// Second TryClaimSlot call (after cleanup) succeeds
	claimCalled := 0
	holdRepo.TryClaimSlotFn = func(ctx context.Context, hold *domain.SlotHold) (*domain.SlotHold, error) {
		claimCalled++
		if claimCalled == 2 {
			hold.ID = newHoldID
			return hold, nil
		}
		return nil, fmt.Errorf("duplicate hold")
	}

	confirmedCalls := 0
	pendingCalls := 0
	bookingRepo := &mockBookingRepoForHold{
		FindConfirmedBySlotFn: func(ctx context.Context, cID primitive.ObjectID, d time.Time, h int) (*domain.Booking, error) {
			confirmedCalls++
			return nil, nil
		},
		FindPendingBySlotFn: func(ctx context.Context, cID primitive.ObjectID, d time.Time, h int) (*domain.Booking, error) {
			pendingCalls++
			return nil, nil
		},
	}

	uc := &BookingUseCase{repo: bookingRepo, holdRepo: holdRepo}

	hold, booking, err := uc.ClaimOrRenewSlot(context.Background(), courtID, date, hour, userID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hold == nil {
		t.Fatal("expected non-nil hold after cleanup")
	}
	if hold.ID != newHoldID {
		t.Errorf("expected new hold ID %s, got %s", newHoldID.Hex(), hold.ID.Hex())
	}
	if booking != nil {
		t.Error("expected nil booking after cleanup")
	}
	if holdRepo.FindOneAndDeleteIfExpiredCalls != 1 {
		t.Errorf("expected 1 FindOneAndDeleteIfExpired call, got %d", holdRepo.FindOneAndDeleteIfExpiredCalls)
	}
	if len(bookingRepo.MarkExpiredCalls) != 1 {
		t.Errorf("expected 1 MarkExpired call, got %d", len(bookingRepo.MarkExpiredCalls))
	}
	if bookingRepo.MarkExpiredCalls[0] != expiredBookingID {
		t.Errorf("expected MarkExpired for booking %s, got %s", expiredBookingID.Hex(), bookingRepo.MarkExpiredCalls[0].Hex())
	}
	// confirmed and pending should be checked twice (first attempt + after cleanup)
	if confirmedCalls != 2 {
		t.Errorf("expected 2 FindConfirmedBySlot calls, got %d", confirmedCalls)
	}
	if pendingCalls != 2 {
		t.Errorf("expected 2 FindPendingBySlot calls, got %d", pendingCalls)
	}
}

func TestClaimOrRenewSlot_PendingWithExpiredLock(t *testing.T) {
	courtID := newObjectID()
	date := chileDate(2026, 5, 13)
	hour := 10
	userID := "device:abc"

	pastLock := pastLockTime()
	oldHoldID := newObjectID()
	expiredBooking := &domain.Booking{
		ID:            newObjectID(),
		GuestDeviceID: "device:other",
		LockExpiresAt: &pastLock,
		HoldID:        &oldHoldID,
	}

	newHoldID := newObjectID()

	bookingRepo := &mockBookingRepoForHold{
		FindConfirmedBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
			return nil, nil
		},
		FindPendingBySlotFn: func(ctx context.Context, courtID primitive.ObjectID, date time.Time, hour int) (*domain.Booking, error) {
			return expiredBooking, nil
		},
	}
	holdRepo := &mockSlotHoldRepo{
		TryClaimSlotFn: func(ctx context.Context, hold *domain.SlotHold) (*domain.SlotHold, error) {
			hold.ID = newHoldID
			return hold, nil
		},
	}

	uc := &BookingUseCase{repo: bookingRepo, holdRepo: holdRepo}

	hold, booking, err := uc.ClaimOrRenewSlot(context.Background(), courtID, date, hour, userID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hold == nil {
		t.Fatal("expected non-nil hold")
	}
	if hold.ID != newHoldID {
		t.Errorf("expected hold ID %s, got %s", newHoldID.Hex(), hold.ID.Hex())
	}
	if booking != nil {
		t.Error("expected nil booking after expired lock cleanup")
	}
	if len(holdRepo.DeleteCalls) != 1 {
		t.Errorf("expected 1 Delete call for old hold, got %d", len(holdRepo.DeleteCalls))
	}
	if holdRepo.DeleteCalls[0] != oldHoldID {
		t.Errorf("expected Delete for hold %s, got %s", oldHoldID.Hex(), holdRepo.DeleteCalls[0].Hex())
	}
	if len(bookingRepo.MarkExpiredCalls) != 1 {
		t.Errorf("expected 1 MarkExpired call, got %d", len(bookingRepo.MarkExpiredCalls))
	}
	if bookingRepo.MarkExpiredCalls[0] != expiredBooking.ID {
		t.Errorf("expected MarkExpired for booking %s, got %s", expiredBooking.ID.Hex(), bookingRepo.MarkExpiredCalls[0].Hex())
	}
	if holdRepo.TryClaimSlotCalls != 1 {
		t.Errorf("expected 1 TryClaimSlot call, got %d", holdRepo.TryClaimSlotCalls)
	}
}
