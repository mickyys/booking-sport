package domain

import "errors"

var ErrNotFound = errors.New("not found")

// ErrBookingSlotTaken indica que ya existe una reserva (o bloqueo) para el
// mismo horario. Se usa en la generación de recurrencias para distinguir un
// conflicto esperable de un error de infraestructura.
var ErrBookingSlotTaken = errors.New("booking slot already taken")

// ErrVersionConflict indica que el documento cambió entre la lectura y la
// escritura. El llamador debe releer y reintentar la transición.
var ErrVersionConflict = errors.New("booking version conflict")

// ErrDiscountUnavailable indica que el descuento dejó de estar disponible
// entre la cotización y la reserva (cupo agotado, límite por usuario o
// vigencia). La reserva puede continuar sin el beneficio.
var ErrDiscountUnavailable = errors.New("discount no longer available")

type AppError struct {
	Message    string
	StatusCode int
	// Details viaja al cliente junto al mensaje (por ejemplo, cuándo se libera
	// el horario). Es opcional.
	Details map[string]any
}

func (e *AppError) Error() string {
	return e.Message
}

func NewConflictError(message string) *AppError {
	return &AppError{Message: message, StatusCode: 409}
}

// NewConflictErrorWithDetails es un 409 con datos estructurados para que el
// frontend no tenga que interpretar el texto.
func NewConflictErrorWithDetails(message string, details map[string]any) *AppError {
	return &AppError{Message: message, StatusCode: 409, Details: details}
}

func NewBadRequestError(message string) *AppError {
	return &AppError{Message: message, StatusCode: 400}
}

func NewNotFoundError(message string) *AppError {
	return &AppError{Message: message, StatusCode: 404}
}
