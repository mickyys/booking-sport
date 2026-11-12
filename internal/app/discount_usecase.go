package app

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// DiscountUseCase agrupa la creación, anulación y consulta de códigos de
// descuento de un centro.
type DiscountUseCase struct {
	repo       DiscountRepository
	centerRepo SportCenterRepository
}

func NewDiscountUseCase(repo DiscountRepository, centerRepo SportCenterRepository) *DiscountUseCase {
	return &DiscountUseCase{repo: repo, centerRepo: centerRepo}
}

// DiscountCodeView agrega el estado derivado y los datos de vigencia que
// consume el panel.
type DiscountCodeView struct {
	domain.DiscountCode
	DerivedStatus string `json:"derived_status"`
}

// CreateCodeInput son los datos editables de un código. El código es opcional:
// si viene vacío se genera automáticamente.
type CreateCodeInput struct {
	Code       string
	Discount   domain.DiscountValue
	Scope      domain.DiscountScope
	UsageLimit domain.UsageLimit
	MinAmount  float64
	ValidFrom  *time.Time
	ValidTo    *time.Time
}

// BatchCodeInput define la generación masiva de códigos.
type BatchCodeInput struct {
	Count      int
	Prefix     string
	Discount   domain.DiscountValue
	Scope      domain.DiscountScope
	UsageLimit domain.UsageLimit
	MinAmount  float64
	ValidFrom  *time.Time
	ValidTo    *time.Time
}

// CreateCode crea un único código para el centro.
func (uc *DiscountUseCase) CreateCode(ctx context.Context, centerID primitive.ObjectID, input CreateCodeInput, createdBy string) (*domain.DiscountCode, error) {
	code := strings.ToUpper(strings.TrimSpace(input.Code))
	if code == "" {
		generated, err := generateDiscountCode("")
		if err != nil {
			return nil, err
		}
		code = generated
	}
	doc := &domain.DiscountCode{
		ID:            primitive.NewObjectID(),
		SportCenterID: centerID,
		Code:          code,
		Discount:      input.Discount,
		Scope:         input.Scope,
		UsageLimit:    input.UsageLimit,
		MinAmount:     input.MinAmount,
		ValidFrom:     input.ValidFrom,
		ValidTo:       input.ValidTo,
		Status:        domain.DiscountCodeStatusActive,
		CreatedBy:     createdBy,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := validateDiscountCode(doc); err != nil {
		return nil, err
	}
	if err := uc.repo.CreateCode(ctx, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// CreateBatch genera N códigos con la misma configuración y un batch_id común
// para poder anularlos o exportarlos juntos.
func (uc *DiscountUseCase) CreateBatch(ctx context.Context, centerID primitive.ObjectID, input BatchCodeInput, createdBy string) ([]domain.DiscountCode, error) {
	if input.Count <= 0 {
		return nil, domain.NewBadRequestError("debes generar al menos un código")
	}
	if input.Count > 500 {
		return nil, domain.NewBadRequestError("el máximo es 500 códigos por lote")
	}
	prefix := strings.ToUpper(strings.TrimSpace(input.Prefix))
	if prefix != "" && !isValidCodePart(prefix) {
		return nil, domain.NewBadRequestError("el prefijo solo puede tener letras, números y guiones")
	}
	if len(prefix) > 12 {
		return nil, domain.NewBadRequestError("el prefijo no puede superar 12 caracteres")
	}

	batchID := primitive.NewObjectID().Hex()
	now := time.Now()
	codes := make([]domain.DiscountCode, 0, input.Count)
	seen := make(map[string]struct{}, input.Count)
	for len(codes) < input.Count {
		generated, err := generateDiscountCode(prefix)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[generated]; ok {
			continue
		}
		seen[generated] = struct{}{}
		doc := domain.DiscountCode{
			ID:            primitive.NewObjectID(),
			SportCenterID: centerID,
			Code:          generated,
			BatchID:       batchID,
			Discount:      input.Discount,
			Scope:         input.Scope,
			UsageLimit:    input.UsageLimit,
			MinAmount:     input.MinAmount,
			ValidFrom:     input.ValidFrom,
			ValidTo:       input.ValidTo,
			Status:        domain.DiscountCodeStatusActive,
			CreatedBy:     createdBy,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := validateDiscountCode(&doc); err != nil {
			return nil, err
		}
		codes = append(codes, doc)
	}
	if err := uc.repo.CreateCodes(ctx, codes); err != nil {
		return nil, err
	}
	return codes, nil
}

// ListCodes devuelve los códigos con su estado visible.
func (uc *DiscountUseCase) ListCodes(ctx context.Context, centerID primitive.ObjectID) ([]DiscountCodeView, error) {
	codes, err := uc.repo.FindCodesByCenter(ctx, centerID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	views := make([]DiscountCodeView, 0, len(codes))
	for _, c := range codes {
		views = append(views, DiscountCodeView{DiscountCode: c, DerivedStatus: c.DerivedStatus(now)})
	}
	return views, nil
}

// VoidCode anula un código. Los usos ya registrados no se modifican.
func (uc *DiscountUseCase) VoidCode(ctx context.Context, centerID primitive.ObjectID, id primitive.ObjectID) error {
	code, err := uc.repo.FindCodeByID(ctx, id)
	if err != nil || code == nil || code.SportCenterID != centerID {
		return domain.NewNotFoundError("código no encontrado")
	}
	return uc.repo.UpdateCodeStatus(ctx, id, domain.DiscountCodeStatusVoided)
}

// VoidBatch anula todos los códigos de un lote.
func (uc *DiscountUseCase) VoidBatch(ctx context.Context, centerID primitive.ObjectID, batchID string) error {
	if strings.TrimSpace(batchID) == "" {
		return domain.NewBadRequestError("lote inválido")
	}
	codes, err := uc.repo.FindCodesByBatch(ctx, centerID, batchID)
	if err != nil {
		return err
	}
	if len(codes) == 0 {
		return domain.NewNotFoundError("lote no encontrado")
	}
	return uc.repo.UpdateBatchStatus(ctx, centerID, batchID, domain.DiscountCodeStatusVoided)
}

// ListUsages devuelve los usos del centro, opcionalmente filtrados por código.
func (uc *DiscountUseCase) ListUsages(ctx context.Context, centerID primitive.ObjectID, codeID string, limit int) ([]domain.DiscountUsage, error) {
	if strings.TrimSpace(codeID) != "" {
		id, err := primitive.ObjectIDFromHex(strings.TrimSpace(codeID))
		if err != nil {
			return nil, domain.NewBadRequestError("code_id inválido")
		}
		code, err := uc.repo.FindCodeByID(ctx, id)
		if err != nil || code == nil || code.SportCenterID != centerID {
			return nil, domain.NewNotFoundError("código no encontrado")
		}
		return uc.repo.FindUsagesByCodeID(ctx, id)
	}
	return uc.repo.FindUsagesByCenter(ctx, centerID, limit)
}

func validateDiscountCode(code *domain.DiscountCode) error {
	if !isValidCodePart(code.Code) {
		return domain.NewBadRequestError("el código solo puede tener letras, números y guiones")
	}
	if len(code.Code) < 3 || len(code.Code) > 32 {
		return domain.NewBadRequestError("el código debe tener entre 3 y 32 caracteres")
	}
	if !code.Discount.Type.IsValid() {
		return domain.NewBadRequestError("el tipo de descuento debe ser PERCENT o FIXED")
	}
	if code.Discount.Value <= 0 {
		return domain.NewBadRequestError("el descuento debe ser mayor que cero")
	}
	if code.Discount.Type == domain.DiscountTypePercent && code.Discount.Value > 100 {
		return domain.NewBadRequestError("el descuento porcentual no puede superar 100%")
	}
	if code.MinAmount < 0 {
		return domain.NewBadRequestError("el monto mínimo no puede ser negativo")
	}
	code.Scope.Normalize()
	if !code.Scope.Mode.IsValid() {
		return domain.NewBadRequestError("el alcance del código es inválido")
	}
	if code.Scope.Mode == domain.DiscountScopeSpaces && len(code.Scope.SpaceIDs) == 0 {
		return domain.NewBadRequestError("selecciona al menos un espacio para el código")
	}
	if code.Scope.Mode == domain.DiscountScopeCategory && len(code.Scope.Categories) == 0 {
		return domain.NewBadRequestError("selecciona al menos una categoría para el código")
	}
	code.UsageLimit.Normalize()
	if !code.UsageLimit.Period.IsValid() {
		return domain.NewBadRequestError("el período del límite es inválido")
	}
	if code.ValidFrom != nil && code.ValidTo != nil && code.ValidTo.Before(*code.ValidFrom) {
		return domain.NewBadRequestError("la fecha de término debe ser posterior al inicio")
	}
	return nil
}

const discountCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func generateDiscountCode(prefix string) (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("no se pudo generar el código: %w", err)
	}
	suffix := make([]byte, len(buf))
	for i, b := range buf {
		suffix[i] = discountCodeAlphabet[int(b)%len(discountCodeAlphabet)]
	}
	if prefix == "" {
		return string(suffix), nil
	}
	return prefix + "-" + string(suffix), nil
}

func isValidCodePart(code string) bool {
	if code == "" {
		return false
	}
	for _, r := range code {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
	}
	return true
}
