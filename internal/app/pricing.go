package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// QuoteInput son los datos necesarios para cotizar una reserva.
type QuoteInput struct {
	CourtID         primitive.ObjectID
	Date            time.Time
	Hour            int
	Minutes         int
	DurationMinutes int
	MemberToken     string
	MemberID        string // solo flujo administrativo
	DiscountCode    string
	IdentityEmail   string
	IdentityUserID  string
	IdentityDevice  string
}

// QuoteResult envuelve la cotización visible y los datos internos necesarios
// para reservar el beneficio de forma atómica.
type QuoteResult struct {
	Quote        *domain.PriceQuote
	CenterID     primitive.ObjectID
	IdentityKey  string
	PeriodKey    string
	Source       string
	SourceID     primitive.ObjectID
	MemberID     primitive.ObjectID
	PlanID       primitive.ObjectID
	Code         string
	MaxUses      int
	PerUserLimit int
}

// PricingService centraliza la resolución de precios y la aplicación de
// descuentos. Es el único lugar que decide el precio final de una reserva.
type PricingService struct {
	courtRepo        CourtRepository
	centerRepo       SportCenterRepository
	spaceRepo        SpaceRepository
	scheduleResolver *ScheduleResolver
	membershipRepo   MembershipRepository
	discountRepo     DiscountRepository
}

func NewPricingService(
	courtRepo CourtRepository,
	centerRepo SportCenterRepository,
	spaceRepo SpaceRepository,
	scheduleResolver *ScheduleResolver,
	membershipRepo MembershipRepository,
	discountRepo DiscountRepository,
) *PricingService {
	return &PricingService{
		courtRepo:        courtRepo,
		centerRepo:       centerRepo,
		spaceRepo:        spaceRepo,
		scheduleResolver: scheduleResolver,
		membershipRepo:   membershipRepo,
		discountRepo:     discountRepo,
	}
}

// BasePrice resuelve el precio lista de un horario. Usa las reglas semanales
// y cae al horario legacy de la cancha.
func (s *PricingService) BasePrice(ctx context.Context, court *domain.Court, date time.Time, hour, minutes, duration int) (float64, error) {
	if court.IsFacility() && court.DayPrice() > 0 {
		return court.DayPrice(), nil
	}
	if duration <= 0 {
		duration = 60
	}
	if s.scheduleResolver != nil {
		franja, hasRules, err := s.scheduleResolver.FindFranjaForBooking(ctx, court.ID, date, hour, minutes, duration)
		if err != nil {
			return 0, err
		}
		if hasRules {
			if franja == nil {
				return 0, fmt.Errorf("el horario %02d:%02d (%d min) no está dentro de la configuración de la cancha", hour, minutes, duration)
			}
			return franja.Price, nil
		}
	}
	for _, slot := range court.Schedule {
		if slot.Hour == hour && slot.Minutes == minutes {
			return slot.Price, nil
		}
	}
	return 0, fmt.Errorf("hour %02d:%02d not found in schedule", hour, minutes)
}

// Quote resuelve el precio y los descuentos de una reserva.
func (s *PricingService) Quote(ctx context.Context, input QuoteInput) (*QuoteResult, error) {
	court, err := s.courtRepo.FindAnyByID(ctx, input.CourtID)
	if err != nil {
		return nil, fmt.Errorf("court not found: %w", err)
	}
	center, err := s.centerRepo.FindByID(ctx, court.SportCenterID)
	if err != nil {
		return nil, fmt.Errorf("sport center not found: %w", err)
	}
	base, err := s.BasePrice(ctx, court, input.Date, input.Hour, input.Minutes, input.DurationMinutes)
	if err != nil {
		return nil, err
	}
	return s.QuoteWithBase(ctx, court, center, base, input)
}

// QuoteWithBase aplica descuentos a un precio ya resuelto. Lo usa el flujo de
// Mercado Pago, que necesita resolver antes las reglas de pago de la franja.
func (s *PricingService) QuoteWithBase(ctx context.Context, court *domain.Court, center *domain.SportCenter, base float64, input QuoteInput) (*QuoteResult, error) {
	now := time.Now()
	loc := centerLocation(center)
	identity := pricingIdentityKey(input.IdentityEmail, input.IdentityUserID, input.IdentityDevice)
	category := s.spaceCategory(ctx, court)

	result := &QuoteResult{
		CenterID: center.ID,
		Quote: &domain.PriceQuote{
			BasePrice:     roundMoney(base),
			DiscountTotal: 0,
			FinalPrice:    roundMoney(base),
			Applied:       []domain.AppliedDiscount{},
			Rejected:      []domain.RejectedDiscount{},
		},
	}

	var memberAmount float64
	var member *domain.Member
	var plan *domain.MembershipPlan
	var memberPeriodKey string

	if s.membershipRepo != nil && (strings.TrimSpace(input.MemberToken) != "" || strings.TrimSpace(input.MemberID) != "") {
		member, plan = s.resolveMember(ctx, center.ID, input.MemberToken, input.MemberID)
		if member == nil || plan == nil {
			if input.MemberToken != "" || input.MemberID != "" {
				result.Quote.Rejected = append(result.Quote.Rejected, domain.RejectedDiscount{
					Kind:   domain.DiscountSourcePlan,
					Reason: "no pudimos validar tu condición de socio; verifica el código enviado a tu correo",
				})
			}
		} else if !member.IsUsable(now) {
			result.Quote.Rejected = append(result.Quote.Rejected, domain.RejectedDiscount{
				Kind:   domain.DiscountSourcePlan,
				Reason: "tu membresía no está vigente",
			})
			member, plan = nil, nil
		} else if !plan.Active {
			result.Quote.Rejected = append(result.Quote.Rejected, domain.RejectedDiscount{
				Kind:   domain.DiscountSourcePlan,
				Reason: "el plan de socio no está activo",
			})
			member, plan = nil, nil
		} else if !plan.Scope.Covers(category, court.ID) {
			result.Quote.Rejected = append(result.Quote.Rejected, domain.RejectedDiscount{
				Kind:   domain.DiscountSourcePlan,
				Reason: "tu plan no aplica a este espacio",
			})
			member, plan = nil, nil
		} else {
			memberPeriodKey = domain.PeriodKey(plan.UsageLimit.Period, now, loc)
			if plan.UsageLimit.MaxUsesPerUser > 0 && s.discountRepo != nil {
				memberIdentity := memberIdentityKey(member)
				counter, err := s.discountRepo.FindCounter(ctx, domain.DiscountSourcePlan, plan.ID, memberIdentity, memberPeriodKey)
				if err == nil && counter != nil && counter.Reserved+counter.Confirmed >= plan.UsageLimit.MaxUsesPerUser {
					result.Quote.Rejected = append(result.Quote.Rejected, domain.RejectedDiscount{
						Kind:   domain.DiscountSourcePlan,
						Reason: "ya usaste el máximo de descuentos de socio para este período",
					})
					member, plan = nil, nil
				}
			}
			if member != nil {
				memberAmount = domain.DiscountAmountFor(base, plan.Discount)
			}
		}
	}

	var codeAmount float64
	var code *domain.DiscountCode
	var codePeriodKey string
	normalizedCode := normalizeDiscountCode(input.DiscountCode)

	if normalizedCode != "" && s.discountRepo != nil {
		found, err := s.discountRepo.FindCodeByCode(ctx, center.ID, normalizedCode)
		if err != nil {
			result.Quote.Rejected = append(result.Quote.Rejected, domain.RejectedDiscount{
				Kind:   domain.DiscountSourceCode,
				Code:   normalizedCode,
				Reason: "código no encontrado",
			})
		} else if !found.IsUsable(now) {
			result.Quote.Rejected = append(result.Quote.Rejected, domain.RejectedDiscount{
				Kind:   domain.DiscountSourceCode,
				Code:   normalizedCode,
				Reason: discountCodeUnavailableReason(found, now),
			})
		} else if !found.Scope.Covers(category, court.ID) {
			result.Quote.Rejected = append(result.Quote.Rejected, domain.RejectedDiscount{
				Kind:   domain.DiscountSourceCode,
				Code:   normalizedCode,
				Reason: "el código no aplica a este espacio",
			})
		} else if found.MinAmount > 0 && base < found.MinAmount {
			result.Quote.Rejected = append(result.Quote.Rejected, domain.RejectedDiscount{
				Kind:   domain.DiscountSourceCode,
				Code:   normalizedCode,
				Reason: fmt.Sprintf("el código requiere un monto mínimo de $%s", formatMoney(found.MinAmount)),
			})
		} else if found.UsageLimit.MaxUsesPerUser > 0 && identity == "" {
			result.Quote.Rejected = append(result.Quote.Rejected, domain.RejectedDiscount{
				Kind:   domain.DiscountSourceCode,
				Code:   normalizedCode,
				Reason: "para usar este código necesitamos tu correo",
			})
		} else {
			codePeriodKey = domain.PeriodKey(found.UsageLimit.Period, now, loc)
			if found.UsageLimit.MaxUsesPerUser > 0 {
				counter, err := s.discountRepo.FindCounter(ctx, domain.DiscountSourceCode, found.ID, identity, codePeriodKey)
				if err == nil && counter != nil && counter.Reserved+counter.Confirmed >= found.UsageLimit.MaxUsesPerUser {
					result.Quote.Rejected = append(result.Quote.Rejected, domain.RejectedDiscount{
						Kind:   domain.DiscountSourceCode,
						Code:   normalizedCode,
						Reason: "ya usaste este código el máximo de veces permitido",
					})
					found = nil
				}
			}
			if found != nil {
				code = found
				codeAmount = domain.DiscountAmountFor(base, code.Discount)
			}
		}
	}

	// No acumulación: se aplica el mayor beneficio. En empate gana el plan de
	// socio para no consumir un código limitado.
	if member != nil && plan != nil && memberAmount >= codeAmount {
		result.Source = domain.DiscountSourcePlan
		result.SourceID = plan.ID
		result.MemberID = member.ID
		result.PlanID = plan.ID
		result.PeriodKey = memberPeriodKey
		result.PerUserLimit = plan.UsageLimit.MaxUsesPerUser
		result.IdentityKey = memberIdentityKey(member)
		result.Quote.Applied = append(result.Quote.Applied, domain.AppliedDiscount{
			Kind:   "membership",
			ID:     plan.ID.Hex(),
			Label:  plan.Name,
			Type:   string(plan.Discount.Type),
			Value:  plan.Discount.Value,
			Amount: memberAmount,
		})
		result.Quote.Member = &domain.QuoteMember{
			ID:       member.ID.Hex(),
			Name:     member.Name,
			PlanID:   plan.ID.Hex(),
			PlanName: plan.Name,
		}
		if code != nil {
			result.Quote.Rejected = append(result.Quote.Rejected, domain.RejectedDiscount{
				Kind:   domain.DiscountSourceCode,
				Code:   code.Code,
				Reason: "se aplicó el descuento de socio por ser mayor",
			})
		}
	} else if code != nil {
		result.Source = domain.DiscountSourceCode
		result.SourceID = code.ID
		result.Code = code.Code
		result.MaxUses = code.UsageLimit.MaxUses
		result.PerUserLimit = code.UsageLimit.MaxUsesPerUser
		result.IdentityKey = identity
		result.PeriodKey = codePeriodKey
		result.Quote.Applied = append(result.Quote.Applied, domain.AppliedDiscount{
			Kind:   "code",
			ID:     code.ID.Hex(),
			Code:   code.Code,
			Label:  "Código " + code.Code,
			Type:   string(code.Discount.Type),
			Value:  code.Discount.Value,
			Amount: codeAmount,
		})
		if member != nil && plan != nil {
			result.Quote.Rejected = append(result.Quote.Rejected, domain.RejectedDiscount{
				Kind:   domain.DiscountSourcePlan,
				Reason: "se aplicó el código por ser un descuento mayor",
			})
		}
	}

	if len(result.Quote.Applied) > 0 {
		result.Quote.DiscountTotal = roundMoney(result.Quote.Applied[0].Amount)
	}
	result.Quote.FinalPrice = roundMoney(math.Max(0, base-result.Quote.DiscountTotal))
	return result, nil
}

// Reserve reserva los usos del beneficio aplicado. Debe llamarse antes de
// persistir la reserva; si la persistencia falla, el llamador debe invocar
// Release.
func (s *PricingService) Reserve(ctx context.Context, result *QuoteResult, booking *domain.Booking) error {
	if result == nil || result.Quote == nil || len(result.Quote.Applied) == 0 || result.Source == "" {
		return nil
	}
	if s.discountRepo == nil {
		return nil
	}

	codeReserved := false
	if result.Source == domain.DiscountSourceCode {
		ok, err := s.discountRepo.TryIncrementCodeUsed(ctx, result.SourceID, result.MaxUses)
		if err != nil {
			return err
		}
		if !ok {
			return domain.ErrDiscountUnavailable
		}
		codeReserved = true
	}

	if result.PerUserLimit > 0 {
		ok, err := s.discountRepo.TryReserveCounter(ctx, result.CenterID, result.Source, result.SourceID, result.IdentityKey, result.PeriodKey, result.PerUserLimit)
		if err != nil {
			if codeReserved {
				_ = s.discountRepo.ReleaseCodeUsed(ctx, result.SourceID)
			}
			return err
		}
		if !ok {
			if codeReserved {
				_ = s.discountRepo.ReleaseCodeUsed(ctx, result.SourceID)
			}
			return domain.ErrDiscountUnavailable
		}
	}

	now := time.Now()
	usage := &domain.DiscountUsage{
		SportCenterID:  result.CenterID,
		Source:         result.Source,
		SourceID:       result.SourceID,
		IdentityKey:    result.IdentityKey,
		PeriodKey:      result.PeriodKey,
		BookingID:      booking.ID,
		MemberID:       result.MemberID,
		Code:           result.Code,
		Status:         domain.DiscountUsageReserved,
		BaseAmount:     result.Quote.BasePrice,
		DiscountAmount: result.Quote.DiscountTotal,
		FinalAmount:    result.Quote.FinalPrice,
		CustomerName:   booking.CustomerName,
		CustomerPhone:  booking.CustomerPhone,
		BookingCode:    booking.BookingCode,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if booking.GuestDetails != nil {
		usage.CustomerEmail = normalizeEmail(booking.GuestDetails.Email)
		if usage.CustomerName == "" {
			usage.CustomerName = booking.GuestDetails.Name
		}
		if usage.CustomerPhone == "" {
			usage.CustomerPhone = booking.GuestDetails.Phone
		}
	}
	if err := s.discountRepo.CreateUsage(ctx, usage); err != nil {
		_ = s.releaseReservation(ctx, result)
		return err
	}
	return nil
}

// Confirm confirma el uso de un descuento cuando el pago se acredita.
func (s *PricingService) Confirm(ctx context.Context, bookingID primitive.ObjectID) error {
	if s.discountRepo == nil {
		return nil
	}
	usage, err := s.discountRepo.FindUsageByBookingID(ctx, bookingID)
	if err != nil || usage == nil || usage.Status != domain.DiscountUsageReserved {
		return nil
	}
	if usage.PeriodKey != "" {
		if err := s.discountRepo.ConfirmCounter(ctx, usage.Source, usage.SourceID, usage.IdentityKey, usage.PeriodKey); err != nil {
			return err
		}
	}
	return s.discountRepo.UpdateUsageStatus(ctx, bookingID, domain.DiscountUsageConfirmed)
}

// Release libera el uso de un descuento cuando la reserva se cancela o expira.
func (s *PricingService) Release(ctx context.Context, bookingID primitive.ObjectID) error {
	if s.discountRepo == nil {
		return nil
	}
	usage, err := s.discountRepo.FindUsageByBookingID(ctx, bookingID)
	if err != nil || usage == nil {
		return nil
	}
	switch usage.Status {
	case domain.DiscountUsageReserved:
		if usage.PeriodKey != "" {
			if err := s.discountRepo.ReleaseCounter(ctx, usage.Source, usage.SourceID, usage.IdentityKey, usage.PeriodKey); err != nil {
				return err
			}
		}
	case domain.DiscountUsageConfirmed:
		// La reserva se pagó y luego se canceló: se devuelve el cupo.
		if usage.PeriodKey != "" {
			if err := s.discountRepo.ReleaseConfirmedCounter(ctx, usage.Source, usage.SourceID, usage.IdentityKey, usage.PeriodKey); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	if usage.Source == domain.DiscountSourceCode {
		if err := s.discountRepo.ReleaseCodeUsed(ctx, usage.SourceID); err != nil {
			return err
		}
	}
	return s.discountRepo.UpdateUsageStatus(ctx, bookingID, domain.DiscountUsageReleased)
}

func (s *PricingService) releaseReservation(ctx context.Context, result *QuoteResult) error {
	if result.PerUserLimit > 0 {
		_ = s.discountRepo.ReleaseCounter(ctx, result.Source, result.SourceID, result.IdentityKey, result.PeriodKey)
	}
	if result.Source == domain.DiscountSourceCode {
		_ = s.discountRepo.ReleaseCodeUsed(ctx, result.SourceID)
	}
	return nil
}

func (s *PricingService) spaceCategory(ctx context.Context, court *domain.Court) domain.SpaceCategory {
	if s.spaceRepo == nil {
		return domain.SpaceCategoryCancha
	}
	space, err := s.spaceRepo.FindByID(ctx, court.ID)
	if err != nil || space == nil || space.Category == "" {
		return domain.SpaceCategoryCancha
	}
	return space.Category
}

func (s *PricingService) resolveMember(ctx context.Context, centerID primitive.ObjectID, token, memberID string) (*domain.Member, *domain.MembershipPlan) {
	if s.membershipRepo == nil {
		return nil, nil
	}
	var member *domain.Member
	if strings.TrimSpace(token) != "" {
		hash := hashToken(token)
		mt, err := s.membershipRepo.FindMemberTokenByHash(ctx, hash)
		if err != nil || mt == nil || mt.SportCenterID != centerID {
			return nil, nil
		}
		m, err := s.membershipRepo.FindMemberByID(ctx, mt.MemberID)
		if err != nil {
			return nil, nil
		}
		member = m
	} else if strings.TrimSpace(memberID) != "" {
		id, err := primitive.ObjectIDFromHex(memberID)
		if err != nil {
			return nil, nil
		}
		m, err := s.membershipRepo.FindMemberByID(ctx, id)
		if err != nil || m.SportCenterID != centerID {
			return nil, nil
		}
		member = m
	} else {
		return nil, nil
	}
	plan, err := s.membershipRepo.FindPlanByID(ctx, member.PlanID)
	if err != nil || plan == nil || plan.SportCenterID != centerID {
		return nil, nil
	}
	return member, plan
}

func memberIdentityKey(member *domain.Member) string {
	if member == nil {
		return ""
	}
	return "member:" + member.ID.Hex()
}

func pricingIdentityKey(email, userID, deviceID string) string {
	if e := normalizeEmail(email); e != "" {
		return e
	}
	if strings.TrimSpace(userID) != "" {
		return "user:" + strings.TrimSpace(userID)
	}
	if strings.TrimSpace(deviceID) != "" {
		return "device:" + strings.TrimSpace(deviceID)
	}
	return ""
}

func normalizeDiscountCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func centerLocation(center *domain.SportCenter) *time.Location {
	if center != nil && strings.TrimSpace(center.Timezone) != "" {
		if loc, err := time.LoadLocation(center.Timezone); err == nil {
			return loc
		}
	}
	return domain.GetSantiagoLocation()
}

func discountCodeUnavailableReason(code *domain.DiscountCode, now time.Time) string {
	switch code.DerivedStatus(now) {
	case domain.DiscountCodeDerivedVoided:
		return "el código fue anulado"
	case domain.DiscountCodeDerivedScheduled:
		return "el código todavía no está vigente"
	case domain.DiscountCodeDerivedExpired:
		return "el código está vencido"
	case domain.DiscountCodeDerivedExhausted:
		return "el código ya alcanzó su máximo de usos"
	default:
		return "el código no está disponible"
	}
}

func roundMoney(v float64) float64 {
	return math.Round(v*100) / 100
}

func formatMoney(v float64) string {
	return fmt.Sprintf("%.0f", v)
}
