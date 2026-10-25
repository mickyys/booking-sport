package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// facilityCategories son las categorías de espacio que se reservan por día.
var facilityCategories = []domain.SpaceCategory{
	domain.SpaceCategoryQuincho,
	domain.SpaceCategorySede,
	domain.SpaceCategoryPiscina,
}

// BookingBackedFacilityStore guarda las reservas de día completo de sedes y
// piscinas en la colección `bookings`, igual que las canchas: así comparten
// agenda, bloqueos, ocupación, pagos, comisión y panel.
//
// Cada día es un Booking (hour=0, duration=1440, booking_code único) y los días
// de una misma compra se agrupan con group_code, que es el código que ve el
// cliente. Durante la transición lee también la colección legacy
// `facility_bookings` (si se entrega) para no perder días ya vendidos.
type BookingBackedFacilityStore struct {
	uc     *BookingUseCase
	legacy FacilityBookingRepository
	now    func() time.Time
}

// NewBookingBackedFacilityStore crea el store. legacy puede ser nil.
func NewBookingBackedFacilityStore(uc *BookingUseCase, legacy FacilityBookingRepository) *BookingBackedFacilityStore {
	return &BookingBackedFacilityStore{uc: uc, legacy: legacy, now: time.Now}
}

var _ FacilityBookingRepository = (*BookingBackedFacilityStore)(nil)

func facilityOwnerKey(email string) string {
	return "fac:" + strings.ToLower(strings.TrimSpace(email))
}

func facilityDayCode(group, localDate string) string {
	return group + "-" + strings.ReplaceAll(localDate, "-", "")
}

func facilityToBooking(fb *domain.FacilityBooking) (*domain.Booking, error) {
	loc := domain.GetSantiagoLocation()
	day, err := time.ParseInLocation("2006-01-02", fb.LocalDate, loc)
	if err != nil {
		return nil, fmt.Errorf("fecha inválida %q: %w", fb.LocalDate, err)
	}
	method := "presential"
	if fb.PaymentMethod == FacilityPaymentOnline {
		method = "mercadopago"
	}
	b := &domain.Booking{
		ID:               fb.ID,
		CourtID:          fb.SpaceID,
		SportCenterID:    fb.SportCenterID,
		CourtName:        fb.SpaceName,
		SportCenterName:  fb.SportCenterName,
		SpaceCategory:    domain.SpaceCategory(fb.SpaceType),
		Guests:           fb.Guests,
		GuestDeviceID:    facilityOwnerKey(fb.CustomerEmail),
		GuestDetails:     &domain.GuestDetails{Name: fb.CustomerName, Email: fb.CustomerEmail, Phone: fb.CustomerPhone},
		CustomerName:     fb.CustomerName,
		CustomerPhone:    fb.CustomerPhone,
		Date:             day,
		Hour:             0,
		Minutes:          0,
		DurationMinutes:  facilityDayMinutes,
		Price:            fb.Price,
		FinalPrice:       fb.Price,
		PendingAmount:    fb.PendingAmount,
		PaidAmount:       fb.PaidAmount,
		IsPartialPayment: fb.IsPartialPayment,
		Status:           fb.Status,
		GroupCode:        fb.BookingCode,
		BookingCode:      facilityDayCode(fb.BookingCode, fb.LocalDate),
		PaymentMethod:    method,
		MPPreferenceID:   fb.MPPreferenceID,
		MPPaymentID:      fb.MPPaymentID,
		LockExpiresAt:    fb.ExpiresAt,
		CreatedAt:        fb.CreatedAt,
		UpdatedAt:        fb.UpdatedAt,
	}
	if err := b.NormalizeSchedule(); err != nil {
		return nil, err
	}
	b.Pricing = &domain.BookingPricing{BasePrice: fb.Price, FinalPrice: fb.Price}
	if fb.CommissionRate > 0 {
		b.Pricing.Commission = &domain.CommissionSnapshot{
			Rate:   fb.CommissionRate,
			Base:   roundMoney(fb.Price),
			Amount: fb.CommissionAmount,
		}
	}
	return b, nil
}

// FacilityBookingToBooking convierte un día de `facility_bookings` en el
// Booking equivalente de `bookings`, conservando su _id (idempotente para la
// migración). Para reservas legacy sin código de grupo usa su booking_code.
func FacilityBookingToBooking(fb *domain.FacilityBooking) (*domain.Booking, error) {
	return facilityToBooking(fb)
}

func bookingToFacility(b *domain.Booking) domain.FacilityBooking {
	method := "venue"
	if b.PaymentMethod == "mercadopago" {
		method = FacilityPaymentOnline
	}
	code := b.GroupCode
	if code == "" {
		code = b.BookingCode
	}
	price := b.FinalPrice
	if price <= 0 {
		price = b.Price
	}
	out := domain.FacilityBooking{
		ID:               b.ID,
		BookingCode:      code,
		SpaceID:          b.CourtID,
		SportCenterID:    b.SportCenterID,
		SpaceName:        b.CourtName,
		SpaceType:        domain.FacilityType(b.SpaceCategory),
		SportCenterName:  b.SportCenterName,
		LocalDate:        b.LocalDate,
		Guests:           b.Guests,
		CustomerName:     b.CustomerName,
		CustomerPhone:    b.CustomerPhone,
		Status:           b.Status,
		Active:           b.Status == domain.BookingStatusPending || b.Status == domain.BookingStatusConfirmed,
		PaymentMethod:    method,
		Price:            price,
		PaidAmount:       b.PaidAmount,
		PendingAmount:    b.PendingAmount,
		IsPartialPayment: b.IsPartialPayment,
		MPPreferenceID:   b.MPPreferenceID,
		MPPaymentID:      b.MPPaymentID,
		ExpiresAt:        b.LockExpiresAt,
		CancelledBy:      b.CancelledBy,
		CreatedAt:        b.CreatedAt,
		UpdatedAt:        b.UpdatedAt,
	}
	if b.GuestDetails != nil {
		out.CustomerName = b.GuestDetails.Name
		out.CustomerEmail = b.GuestDetails.Email
		out.CustomerPhone = b.GuestDetails.Phone
	}
	return out
}

func facilityScope() bson.M {
	return bson.M{"space_category": bson.M{"$in": facilityCategories}}
}

func mergeFilter(base bson.M, extra bson.M) bson.M {
	out := bson.M{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func isSlotTakenErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, domain.ErrBookingSlotTaken) ||
		strings.Contains(err.Error(), "ya existe") ||
		strings.Contains(err.Error(), "duplicate")
}

// CreateMany inserta un Booking por día. Si un día ya está tomado revierte los
// anteriores y devuelve domain.ErrBookingSlotTaken.
func (s *BookingBackedFacilityStore) CreateMany(ctx context.Context, docs []*domain.FacilityBooking) error {
	if s.legacy != nil && len(docs) > 0 {
		dates := make([]string, 0, len(docs))
		for _, d := range docs {
			dates = append(dates, d.LocalDate)
		}
		sort.Strings(dates)
		taken, err := s.legacy.ListActiveDates(ctx, docs[0].SpaceID, dates[0], dates[len(dates)-1], s.now())
		if err != nil {
			return err
		}
		for _, t := range taken {
			for _, d := range dates {
				if t == d {
					return domain.ErrBookingSlotTaken
				}
			}
		}
	}

	created := make([]*domain.Booking, 0, len(docs))
	rollback := func() {
		for _, b := range created {
			s.uc.releaseOccupancy(ctx, b)
			if b.HoldID != nil {
				_ = s.uc.holdRepo.Delete(ctx, *b.HoldID)
			}
			_ = s.uc.repo.Delete(ctx, b.ID)
		}
	}
	for _, fb := range docs {
		b, err := facilityToBooking(fb)
		if err != nil {
			rollback()
			return err
		}
		if err := s.createDay(ctx, b); err != nil {
			rollback()
			return err
		}
		fb.ID = b.ID
		created = append(created, b)
	}
	return nil
}

func (s *BookingBackedFacilityStore) createDay(ctx context.Context, b *domain.Booking) error {
	confirmed, err := s.uc.repo.FindConfirmedBySlot(ctx, b.CourtID, b.Date, 0, 0)
	if err != nil {
		return err
	}
	if confirmed != nil {
		return domain.ErrBookingSlotTaken
	}

	now := s.now()
	pending, err := s.uc.repo.FindPendingBySlot(ctx, b.CourtID, b.Date, 0, 0)
	if err != nil {
		return err
	}
	if pending != nil {
		if pending.LockExpiresAt != nil && now.Before(*pending.LockExpiresAt) {
			return domain.ErrBookingSlotTaken
		}
		if pending.HoldID != nil {
			_ = s.uc.holdRepo.Delete(ctx, *pending.HoldID)
		}
		_ = s.uc.repo.MarkExpired(ctx, pending.ID)
	}

	if b.Status != domain.BookingStatusPending {
		// Pago en el centro: nace confirmada.
		err := s.uc.persistWithOccupancy(ctx, b, func() error { return s.uc.repo.Create(ctx, b) })
		if isSlotTakenErr(err) {
			return domain.ErrBookingSlotTaken
		}
		return err
	}

	// Pago online: se retiene el día mientras el cliente paga.
	expires := now.Add(mpHoldTTL)
	if b.LockExpiresAt != nil {
		expires = *b.LockExpiresAt
	}
	b.LockExpiresAt = &expires
	claim := func() (*domain.SlotHold, error) {
		return s.uc.holdRepo.TryClaimSlot(ctx, &domain.SlotHold{
			CourtID:   b.CourtID,
			Date:      b.Date,
			Hour:      0,
			Minutes:   0,
			UserID:    b.GuestDeviceID,
			ExpiresAt: expires,
			CreatedAt: now,
		})
	}
	hold, err := claim()
	if err != nil && strings.Contains(err.Error(), "duplicate") {
		if stale, _ := s.uc.holdRepo.FindOneAndDeleteIfExpired(ctx, b.CourtID, b.Date, 0, 0); stale != nil {
			hold, err = claim()
		}
	}
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") {
			return domain.ErrBookingSlotTaken
		}
		return err
	}
	b.HoldID = &hold.ID

	if err := s.uc.holdOccupancy(ctx, b, mpHoldTTL); err != nil {
		_ = s.uc.holdRepo.Delete(ctx, hold.ID)
		if isConflict(err) {
			return domain.ErrBookingSlotTaken
		}
		return err
	}
	if err := s.uc.repo.Create(ctx, b); err != nil {
		_ = s.uc.holdRepo.Delete(ctx, hold.ID)
		s.uc.releaseOccupancy(ctx, b)
		if isSlotTakenErr(err) {
			return domain.ErrBookingSlotTaken
		}
		return err
	}
	return nil
}

// ExpireStale expira las reservas pendientes cuyo plazo de pago venció.
func (s *BookingBackedFacilityStore) ExpireStale(ctx context.Context, spaceID primitive.ObjectID, now time.Time) error {
	_, err := s.uc.repo.Collection().UpdateMany(ctx,
		mergeFilter(facilityScope(), bson.M{
			"court_id":        spaceID,
			"status":          domain.BookingStatusPending,
			"lock_expires_at": bson.M{"$lte": now},
		}),
		bson.M{"$set": bson.M{"status": domain.BookingStatusExpired, "expired_at": now, "updated_at": now}},
	)
	if err != nil {
		return err
	}
	if s.legacy != nil {
		return s.legacy.ExpireStale(ctx, spaceID, now)
	}
	return nil
}

func (s *BookingBackedFacilityStore) findMany(ctx context.Context, filter bson.M, opts ...*options.FindOptions) ([]domain.Booking, error) {
	cursor, err := s.uc.repo.Collection().Find(ctx, filter, opts...)
	if err != nil {
		return nil, err
	}
	var out []domain.Booking
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func toFacilityList(in []domain.Booking) []domain.FacilityBooking {
	out := make([]domain.FacilityBooking, 0, len(in))
	for i := range in {
		out = append(out, bookingToFacility(&in[i]))
	}
	return out
}

func (s *BookingBackedFacilityStore) FindByCode(ctx context.Context, code string) ([]domain.FacilityBooking, error) {
	rows, err := s.findMany(ctx,
		mergeFilter(facilityScope(), bson.M{"$or": bson.A{bson.M{"group_code": code}, bson.M{"booking_code": code}}}),
		options.Find().SetSort(bson.D{{Key: "local_date", Value: 1}}))
	if err != nil {
		return nil, err
	}
	out := toFacilityList(rows)
	if len(out) == 0 && s.legacy != nil {
		return s.legacy.FindByCode(ctx, code)
	}
	return out, nil
}

func (s *BookingBackedFacilityStore) FindByMPPaymentID(ctx context.Context, paymentID string) ([]domain.FacilityBooking, error) {
	rows, err := s.findMany(ctx, mergeFilter(facilityScope(), bson.M{"mp_payment_id": paymentID}))
	if err != nil {
		return nil, err
	}
	out := toFacilityList(rows)
	if len(out) == 0 && s.legacy != nil {
		return s.legacy.FindByMPPaymentID(ctx, paymentID)
	}
	return out, nil
}

func (s *BookingBackedFacilityStore) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.FacilityBooking, error) {
	rows, err := s.findMany(ctx, mergeFilter(facilityScope(), bson.M{"_id": id}))
	if err == nil && len(rows) > 0 {
		fb := bookingToFacility(&rows[0])
		return &fb, nil
	}
	if s.legacy != nil {
		return s.legacy.FindByID(ctx, id)
	}
	if err != nil {
		return nil, err
	}
	return nil, errors.New("facility booking not found")
}

func (s *BookingBackedFacilityStore) ListActiveDates(ctx context.Context, spaceID primitive.ObjectID, from, to string, now time.Time) ([]string, error) {
	rows, err := s.findMany(ctx, bson.M{
		"court_id":   spaceID,
		"local_date": bson.M{"$gte": from, "$lte": to},
		"$or": bson.A{
			bson.M{"status": domain.BookingStatusConfirmed},
			bson.M{"status": domain.BookingStatusPending, "lock_expires_at": bson.M{"$gt": now}},
		},
	})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if !seen[r.LocalDate] {
			seen[r.LocalDate] = true
			out = append(out, r.LocalDate)
		}
	}
	if s.legacy != nil {
		old, err := s.legacy.ListActiveDates(ctx, spaceID, from, to, now)
		if err != nil {
			return nil, err
		}
		for _, d := range old {
			if !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *BookingBackedFacilityStore) ListByCenter(ctx context.Context, centerID primitive.ObjectID) ([]domain.FacilityBooking, error) {
	rows, err := s.findMany(ctx,
		mergeFilter(facilityScope(), bson.M{"sport_center_id": centerID}),
		options.Find().SetSort(bson.D{{Key: "local_date", Value: -1}}))
	if err != nil {
		return nil, err
	}
	out := toFacilityList(rows)
	if s.legacy != nil {
		old, err := s.legacy.ListByCenter(ctx, centerID)
		if err != nil {
			return nil, err
		}
		out = append(out, old...)
	}
	return out, nil
}

func (s *BookingBackedFacilityStore) SetPreference(ctx context.Context, code, preferenceID string) error {
	_, err := s.uc.repo.Collection().UpdateMany(ctx,
		mergeFilter(facilityScope(), bson.M{"group_code": code}),
		bson.M{"$set": bson.M{"mp_preference_id": preferenceID, "updated_at": s.now()}})
	if err != nil {
		return err
	}
	if s.legacy != nil {
		return s.legacy.SetPreference(ctx, code, preferenceID)
	}
	return nil
}

func (s *BookingBackedFacilityStore) SetPaymentID(ctx context.Context, code, paymentID string) error {
	_, err := s.uc.repo.Collection().UpdateMany(ctx,
		mergeFilter(facilityScope(), bson.M{"group_code": code}),
		bson.M{"$set": bson.M{"mp_payment_id": paymentID, "updated_at": s.now()}})
	if err != nil {
		return err
	}
	if s.legacy != nil {
		return s.legacy.SetPaymentID(ctx, code, paymentID)
	}
	return nil
}

// Confirm confirma los días pendientes del grupo con el monto pagado por día,
// moviendo antes su ocupación a "committing" para respetar el coordinador.
func (s *BookingBackedFacilityStore) Confirm(ctx context.Context, code string, paidPerDay float64) error {
	rows, err := s.findMany(ctx, mergeFilter(facilityScope(), bson.M{"group_code": code, "status": domain.BookingStatusPending}))
	if err != nil {
		return err
	}
	for i := range rows {
		b := &rows[i]
		if err := s.uc.beginConfirm(ctx, b); err != nil {
			return err
		}
		pending := b.FinalPrice - paidPerDay
		if pending < 0 {
			pending = 0
		}
		_, err := s.uc.repo.Collection().UpdateOne(ctx,
			bson.M{"_id": b.ID, "status": domain.BookingStatusPending},
			bson.M{
				"$set":   bson.M{"status": domain.BookingStatusConfirmed, "paid_amount": paidPerDay, "pending_amount": pending, "updated_at": s.now()},
				"$unset": bson.M{"lock_expires_at": "", "hold_id": ""},
				"$inc":   bson.M{"version": 1},
			})
		if err != nil {
			return err
		}
		if b.HoldID != nil {
			_ = s.uc.holdRepo.Delete(ctx, *b.HoldID)
		}
		s.uc.finishConfirm(ctx, b)
	}
	if s.legacy != nil {
		return s.legacy.Confirm(ctx, code, paidPerDay)
	}
	return nil
}

func (s *BookingBackedFacilityStore) cancelRows(ctx context.Context, rows []domain.Booking, cancelledBy string, status domain.BookingStatus) error {
	for i := range rows {
		b := &rows[i]
		_, err := s.uc.repo.Collection().UpdateOne(ctx,
			bson.M{"_id": b.ID, "status": bson.M{"$in": bson.A{domain.BookingStatusPending, domain.BookingStatusConfirmed}}},
			bson.M{"$set": bson.M{"status": status, "cancelled_by": cancelledBy, "updated_at": s.now()}})
		if err != nil {
			return err
		}
		s.uc.releaseOccupancy(ctx, b)
		if b.HoldID != nil {
			_ = s.uc.holdRepo.Delete(ctx, *b.HoldID)
		}
	}
	return nil
}

func (s *BookingBackedFacilityStore) CancelByCode(ctx context.Context, code, cancelledBy string, status domain.BookingStatus) error {
	rows, err := s.findMany(ctx, mergeFilter(facilityScope(), bson.M{
		"group_code": code,
		"status":     bson.M{"$in": bson.A{domain.BookingStatusPending, domain.BookingStatusConfirmed}},
	}))
	if err != nil {
		return err
	}
	if err := s.cancelRows(ctx, rows, cancelledBy, status); err != nil {
		return err
	}
	if s.legacy != nil {
		return s.legacy.CancelByCode(ctx, code, cancelledBy, status)
	}
	return nil
}

func (s *BookingBackedFacilityStore) CancelByID(ctx context.Context, id primitive.ObjectID, cancelledBy string) error {
	rows, err := s.findMany(ctx, mergeFilter(facilityScope(), bson.M{"_id": id}))
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		return s.cancelRows(ctx, rows, cancelledBy, domain.BookingStatusCancelled)
	}
	if s.legacy != nil {
		return s.legacy.CancelByID(ctx, id, cancelledBy)
	}
	return nil
}
