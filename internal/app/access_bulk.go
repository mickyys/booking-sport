package app

import (
	"context"
	"strings"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	accessBulkMaxIDs    = 200
	accessBulkMaxFilter = 1000
	accessImportMaxRows = 1000
)

// AccessBulkFailure describe una solicitud que no pudo actualizarse.
type AccessBulkFailure struct {
	ID    string `json:"id"`
	RUT   string `json:"rut,omitempty"`
	Error string `json:"error"`
}

// AccessBulkResult resume una acción múltiple.
type AccessBulkResult struct {
	Updated    int                 `json:"updated"`
	UpdatedIDs []string            `json:"-"`
	Skipped    int                 `json:"skipped"`
	Failed     []AccessBulkFailure `json:"failed"`
	Truncated  bool                `json:"truncated,omitempty"`
}

// BulkSetStatus aplica el mismo estado a varias solicitudes. Recibe ids
// explícitos o un filtro; con filtro se omiten las solicitudes cuyo estado no
// admite la transición, y con ids se informan como error.
func (uc *CenterAccessUseCase) BulkSetStatus(ctx context.Context, centerID primitive.ObjectID, ids []primitive.ObjectID, filter *CenterAccessRequestFilter, status, reason, actorID, actorEmail string) (*AccessBulkResult, error) {
	status = strings.TrimSpace(strings.ToLower(status))
	reason = strings.TrimSpace(reason)
	switch status {
	case domain.CenterAccessStatusApproved, domain.CenterAccessStatusRejected, domain.CenterAccessStatusRevoked:
	default:
		return nil, domain.NewBadRequestError("estado inválido")
	}
	if (status == domain.CenterAccessStatusRejected || status == domain.CenterAccessStatusRevoked) && len(reason) < 5 {
		return nil, domain.NewBadRequestError("el motivo es obligatorio (mínimo 5 caracteres)")
	}

	result := &AccessBulkResult{Failed: []AccessBulkFailure{}}
	if filter != nil {
		f := *filter
		f.CenterID = centerID
		f.All = true
		items, _, err := uc.repo.ListRequests(ctx, f)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if !validAccessTransition(item.Status, status) {
				result.Skipped++
				continue
			}
			if result.Updated+len(result.Failed) >= accessBulkMaxFilter {
				result.Truncated = true
				break
			}
			uc.bulkApply(ctx, centerID, item.ID, item.RUT, status, reason, actorID, actorEmail, result)
		}
		return result, nil
	}

	if len(ids) == 0 {
		return nil, domain.NewBadRequestError("selecciona al menos una solicitud")
	}
	if len(ids) > accessBulkMaxIDs {
		return nil, domain.NewBadRequestError("puedes actualizar hasta 200 solicitudes por vez")
	}
	seen := map[primitive.ObjectID]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		uc.bulkApply(ctx, centerID, id, "", status, reason, actorID, actorEmail, result)
	}
	return result, nil
}

func (uc *CenterAccessUseCase) bulkApply(ctx context.Context, centerID, id primitive.ObjectID, rut, status, reason, actorID, actorEmail string, result *AccessBulkResult) {
	_, err := uc.SetRequestStatus(ctx, centerID, id, status, reason, actorID, actorEmail)
	if err != nil {
		result.Failed = append(result.Failed, AccessBulkFailure{ID: id.Hex(), RUT: rut, Error: err.Error()})
		return
	}
	result.Updated++
	result.UpdatedIDs = append(result.UpdatedIDs, id.Hex())
}

// ResendApproval reenvía el correo de aprobación de una solicitud aprobada.
func (uc *CenterAccessUseCase) ResendApproval(ctx context.Context, centerID, id primitive.ObjectID) error {
	request, err := uc.GetRequest(ctx, centerID, id)
	if err != nil {
		return err
	}
	if request.Status != domain.CenterAccessStatusApproved {
		return domain.NewBadRequestError("solo se puede reenviar a solicitudes aprobadas")
	}
	if !isValidEmail(request.Email) {
		return domain.NewBadRequestError("la solicitud no tiene un correo válido")
	}
	center, err := uc.center(ctx, centerID)
	if err != nil {
		return err
	}
	uc.sendApprovedEmail(center, request)
	return nil
}

// AccessImportRow es una fila del archivo de importación.
type AccessImportRow struct {
	Line  int    `json:"line"`
	RUT   string `json:"rut"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// AccessImportError describe una fila rechazada.
type AccessImportError struct {
	Line  int    `json:"line"`
	RUT   string `json:"rut,omitempty"`
	Error string `json:"error"`
}

// AccessImportResult resume una importación (o su simulación).
type AccessImportResult struct {
	DryRun   bool                `json:"dry_run"`
	Total    int                 `json:"total"`
	Valid    int                 `json:"valid"`
	Imported int                 `json:"imported"`
	Errors   []AccessImportError `json:"errors"`
}

// ImportRequests registra y aprueba a varias personas. Con dryRun solo valida.
func (uc *CenterAccessUseCase) ImportRequests(ctx context.Context, centerID primitive.ObjectID, actorID, actorEmail string, rows []AccessImportRow, dryRun bool) (*AccessImportResult, error) {
	if len(rows) == 0 {
		return nil, domain.NewBadRequestError("el archivo no tiene filas")
	}
	if len(rows) > accessImportMaxRows {
		return nil, domain.NewBadRequestError("el archivo admite hasta 1000 filas")
	}
	if _, err := uc.center(ctx, centerID); err != nil {
		return nil, err
	}
	result := &AccessImportResult{DryRun: dryRun, Total: len(rows), Errors: []AccessImportError{}}
	seen := map[string]bool{}
	for _, row := range rows {
		rut := normalizeAccessRUT(row.RUT)
		fail := func(msg string) {
			result.Errors = append(result.Errors, AccessImportError{Line: row.Line, RUT: row.RUT, Error: msg})
		}
		if !validateRUT(rut) {
			fail("RUT inválido")
			continue
		}
		if !isValidEmail(normalizeAccessEmail(row.Email)) {
			fail("correo inválido")
			continue
		}
		if seen[rut] {
			fail("RUT repetido en el archivo")
			continue
		}
		seen[rut] = true
		existing, err := uc.repo.FindRequestByCenterAndRUT(ctx, centerID, rut)
		if err != nil {
			fail(err.Error())
			continue
		}
		if existing != nil && existing.Status == domain.CenterAccessStatusApproved {
			fail("ya tiene acceso aprobado")
			continue
		}
		result.Valid++
		if dryRun {
			continue
		}
		_, err = uc.CreateManualRequest(ctx, centerID, actorID, actorEmail, CenterAccessRequestInput{RUT: row.RUT, Email: row.Email, Name: row.Name})
		if err != nil {
			result.Valid--
			fail(err.Error())
			continue
		}
		result.Imported++
	}
	return result, nil
}
