package app

import (
	"context"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// MembershipUseCase gestiona los planes y socios de un centro. Todo queda
// aislado por sport_center_id: un organizador solo ve los datos de sus centros.
type MembershipUseCase struct {
	repo       MembershipRepository
	centerRepo SportCenterRepository
}

func NewMembershipUseCase(repo MembershipRepository, centerRepo SportCenterRepository) *MembershipUseCase {
	return &MembershipUseCase{repo: repo, centerRepo: centerRepo}
}

// ResolveCenter obtiene el centro sobre el que opera el usuario. Si se indica
// centerID se valida la pertenencia; si no, se usa el primer centro del
// organizador. Evita que un administrador consulte datos de otro centro.
func (uc *MembershipUseCase) ResolveCenter(ctx context.Context, userID, centerID string) (*domain.SportCenter, error) {
	centers, err := uc.centerRepo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(centers) == 0 {
		return nil, domain.NewNotFoundError("no tienes centros deportivos asociados")
	}
	if strings.TrimSpace(centerID) == "" {
		return &centers[0], nil
	}
	id, err := primitive.ObjectIDFromHex(strings.TrimSpace(centerID))
	if err != nil {
		return nil, domain.NewBadRequestError("center_id inválido")
	}
	for i := range centers {
		if centers[i].ID == id {
			return &centers[i], nil
		}
	}
	return nil, domain.NewNotFoundError("centro deportivo no encontrado")
}

// ListPlans devuelve los planes del centro, incluidos los inactivos para que el
// organizador pueda reactivarlos.
func (uc *MembershipUseCase) ListPlans(ctx context.Context, centerID primitive.ObjectID) ([]domain.MembershipPlan, error) {
	return uc.repo.FindPlansByCenter(ctx, centerID)
}

// CreatePlan valida y persiste un plan de socio.
func (uc *MembershipUseCase) CreatePlan(ctx context.Context, centerID primitive.ObjectID, plan *domain.MembershipPlan, createdBy string) (*domain.MembershipPlan, error) {
	if err := validatePlan(plan); err != nil {
		return nil, err
	}
	now := time.Now()
	plan.ID = primitive.NewObjectID()
	plan.SportCenterID = centerID
	plan.CreatedBy = createdBy
	plan.CreatedAt = now
	plan.UpdatedAt = now
	if err := uc.repo.CreatePlan(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

// GetPlan obtiene un plan del centro.
func (uc *MembershipUseCase) GetPlan(ctx context.Context, centerID primitive.ObjectID, id primitive.ObjectID) (*domain.MembershipPlan, error) {
	plan, err := uc.repo.FindPlanByID(ctx, id)
	if err != nil || plan == nil || plan.SportCenterID != centerID {
		return nil, domain.NewNotFoundError("plan no encontrado")
	}
	return plan, nil
}

// UpdatePlan actualiza un plan existente sin permitir moverlo de centro.
func (uc *MembershipUseCase) UpdatePlan(ctx context.Context, centerID primitive.ObjectID, id primitive.ObjectID, patch *domain.MembershipPlan) (*domain.MembershipPlan, error) {
	plan, err := uc.repo.FindPlanByID(ctx, id)
	if err != nil || plan == nil || plan.SportCenterID != centerID {
		return nil, domain.NewNotFoundError("plan no encontrado")
	}
	mergePlan(plan, patch)
	patch.ID = plan.ID
	patch.SportCenterID = centerID
	patch.CreatedBy = plan.CreatedBy
	patch.CreatedAt = plan.CreatedAt
	patch.UpdatedAt = time.Now()
	if err := validatePlan(patch); err != nil {
		return nil, err
	}
	if err := uc.repo.UpdatePlan(ctx, patch); err != nil {
		return nil, err
	}
	return patch, nil
}

// mergePlan conserva los valores actuales cuando el cliente no envía un campo.
func mergePlan(current, patch *domain.MembershipPlan) {
	if patch.Name == "" {
		patch.Name = current.Name
	}
	if patch.Description == "" {
		patch.Description = current.Description
	}
	if patch.Discount.Type == "" {
		patch.Discount = current.Discount
	}
	if patch.Scope.Mode == "" {
		patch.Scope = current.Scope
	}
	if patch.UsageLimit.Period == "" {
		patch.UsageLimit = current.UsageLimit
	}
}

// DeletePlan desactiva el plan. No se borra para preservar la trazabilidad de
// los socios y usos históricos.
func (uc *MembershipUseCase) DeletePlan(ctx context.Context, centerID primitive.ObjectID, id primitive.ObjectID) error {
	plan, err := uc.repo.FindPlanByID(ctx, id)
	if err != nil || plan == nil || plan.SportCenterID != centerID {
		return domain.NewNotFoundError("plan no encontrado")
	}
	return uc.repo.DeletePlan(ctx, id)
}

// ListMembers devuelve los socios del centro con el nombre del plan resuelto.
func (uc *MembershipUseCase) ListMembers(ctx context.Context, centerID primitive.ObjectID) ([]MemberView, error) {
	members, err := uc.repo.FindMembersByCenter(ctx, centerID)
	if err != nil {
		return nil, err
	}
	plans, err := uc.repo.FindPlansByCenter(ctx, centerID)
	if err != nil {
		return nil, err
	}
	planNames := make(map[primitive.ObjectID]string, len(plans))
	for _, p := range plans {
		planNames[p.ID] = p.Name
	}
	views := make([]MemberView, 0, len(members))
	for i := range members {
		views = append(views, MemberView{
			Member:   members[i],
			PlanName: planNames[members[i].PlanID],
		})
	}
	return views, nil
}

// MemberView agrega el nombre del plan al socio para el panel.
type MemberView struct {
	domain.Member
	PlanName string `json:"plan_name"`
}

// CreateMember valida el documento y el plan antes de registrar al socio.
func (uc *MembershipUseCase) CreateMember(ctx context.Context, centerID primitive.ObjectID, member *domain.Member, createdBy string) (*domain.Member, error) {
	if err := uc.validateMember(ctx, centerID, member); err != nil {
		return nil, err
	}
	now := time.Now()
	member.ID = primitive.NewObjectID()
	member.SportCenterID = centerID
	member.DocumentNumber = normalizeDocument(member.DocumentNumber)
	member.Email = normalizeEmail(member.Email)
	member.CreatedBy = createdBy
	member.CreatedAt = now
	member.UpdatedAt = now
	if err := uc.repo.CreateMember(ctx, member); err != nil {
		return nil, err
	}
	return member, nil
}

// UpdateMember actualiza los datos de un socio existente.
func (uc *MembershipUseCase) UpdateMember(ctx context.Context, centerID primitive.ObjectID, id primitive.ObjectID, patch *domain.Member) (*domain.Member, error) {
	member, err := uc.repo.FindMemberByID(ctx, id)
	if err != nil || member == nil || member.SportCenterID != centerID {
		return nil, domain.NewNotFoundError("socio no encontrado")
	}
	mergeMember(member, patch)
	if err := uc.validateMember(ctx, centerID, patch); err != nil {
		return nil, err
	}
	patch.ID = member.ID
	patch.SportCenterID = centerID
	patch.DocumentNumber = normalizeDocument(patch.DocumentNumber)
	patch.Email = normalizeEmail(patch.Email)
	patch.CreatedBy = member.CreatedBy
	patch.CreatedAt = member.CreatedAt
	if err := uc.repo.UpdateMember(ctx, patch); err != nil {
		return nil, err
	}
	return patch, nil
}

// mergeMember conserva los valores actuales cuando el cliente no envía un
// campo, para que una edición parcial no borre datos del socio.
func mergeMember(current, patch *domain.Member) {
	if patch.Name == "" {
		patch.Name = current.Name
	}
	if patch.DocumentNumber == "" {
		patch.DocumentNumber = current.DocumentNumber
	}
	if patch.Email == "" {
		patch.Email = current.Email
	}
	if patch.Phone == "" {
		patch.Phone = current.Phone
	}
	if patch.Auth0ID == "" {
		patch.Auth0ID = current.Auth0ID
	}
	if patch.Notes == "" {
		patch.Notes = current.Notes
	}
	if patch.PlanID.IsZero() {
		patch.PlanID = current.PlanID
	}
	if patch.Status == "" {
		patch.Status = current.Status
	}
	if patch.ValidFrom == nil {
		patch.ValidFrom = current.ValidFrom
	}
	if patch.ValidTo == nil {
		patch.ValidTo = current.ValidTo
	}
}

// DeleteMember elimina el registro del socio.
func (uc *MembershipUseCase) DeleteMember(ctx context.Context, centerID primitive.ObjectID, id primitive.ObjectID) error {
	member, err := uc.repo.FindMemberByID(ctx, id)
	if err != nil || member == nil || member.SportCenterID != centerID {
		return domain.NewNotFoundError("socio no encontrado")
	}
	return uc.repo.DeleteMember(ctx, id)
}

func (uc *MembershipUseCase) validateMember(ctx context.Context, centerID primitive.ObjectID, member *domain.Member) error {
	if member == nil {
		return domain.NewBadRequestError("datos del socio incompletos")
	}
	if strings.TrimSpace(member.Name) == "" {
		return domain.NewBadRequestError("el nombre del socio es obligatorio")
	}
	if !validateRUT(member.DocumentNumber) {
		return domain.NewBadRequestError("el RUT del socio no es válido")
	}
	if !isValidEmail(normalizeEmail(member.Email)) {
		return domain.NewBadRequestError("el correo del socio no es válido")
	}
	if member.PlanID.IsZero() {
		return domain.NewBadRequestError("debes asignar un plan de socio")
	}
	plan, err := uc.repo.FindPlanByID(ctx, member.PlanID)
	if err != nil || plan == nil || plan.SportCenterID != centerID {
		return domain.NewBadRequestError("el plan de socio no existe en este centro")
	}
	if member.Status == "" {
		member.Status = domain.MemberStatusActive
	}
	if !member.Status.IsValid() {
		return domain.NewBadRequestError("estado de socio inválido")
	}
	if member.ValidFrom != nil && member.ValidTo != nil && member.ValidTo.Before(*member.ValidFrom) {
		return domain.NewBadRequestError("la fecha de término debe ser posterior al inicio")
	}
	return nil
}

func validatePlan(plan *domain.MembershipPlan) error {
	if plan == nil {
		return domain.NewBadRequestError("datos del plan incompletos")
	}
	if strings.TrimSpace(plan.Name) == "" {
		return domain.NewBadRequestError("el nombre del plan es obligatorio")
	}
	if !plan.Discount.Type.IsValid() {
		return domain.NewBadRequestError("el tipo de descuento debe ser PERCENT o FIXED")
	}
	if plan.Discount.Value <= 0 {
		return domain.NewBadRequestError("el descuento debe ser mayor que cero")
	}
	if plan.Discount.Type == domain.DiscountTypePercent && plan.Discount.Value > 100 {
		return domain.NewBadRequestError("el descuento porcentual no puede superar 100%")
	}
	plan.Scope.Normalize()
	if !plan.Scope.Mode.IsValid() {
		return domain.NewBadRequestError("el alcance del plan es inválido")
	}
	if plan.Scope.Mode == domain.DiscountScopeSpaces && len(plan.Scope.SpaceIDs) == 0 {
		return domain.NewBadRequestError("selecciona al menos un espacio para el plan")
	}
	if plan.Scope.Mode == domain.DiscountScopeCategory && len(plan.Scope.Categories) == 0 {
		return domain.NewBadRequestError("selecciona al menos una categoría para el plan")
	}
	plan.UsageLimit.Normalize()
	if !plan.UsageLimit.Period.IsValid() {
		return domain.NewBadRequestError("el período del límite es inválido")
	}
	return nil
}
