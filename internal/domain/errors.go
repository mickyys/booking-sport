package domain

import "errors"

var ErrNotFound = errors.New("not found")

// ErrBookingSlotTaken indica que ya existe una reserva (o bloqueo) para el
// mismo horario. Se usa en la generación de recurrencias para distinguir un
// conflicto esperable de un error de infraestructura.
var ErrBookingSlotTaken = errors.New("booking slot already taken")

type AppError struct {
	Message    string
	StatusCode int
}

func (e *AppError) Error() string {
	return e.Message
}

func NewConflictError(message string) *AppError {
	return &AppError{Message: message, StatusCode: 409}
}

func NewBadRequestError(message string) *AppError {
	return &AppError{Message: message, StatusCode: 400}
}

func NewNotFoundError(message string) *AppError {
	return &AppError{Message: message, StatusCode: 404}
}
