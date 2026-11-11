package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	// MaxBookingExportDays es el rango máximo (inclusive) de un reporte.
	MaxBookingExportDays = 92
	// maxBookingExportRows acota el tamaño del archivo generado.
	maxBookingExportRows = 10000
)

var ErrInvalidExportRange = errors.New("rango de fechas inválido")

// BookingExportStore se descubre por type assertion sobre BookingRepository
// para no obligar a todos los dobles de prueba a implementarla.
type BookingExportStore interface {
	FindForExport(ctx context.Context, centerIDs []primitive.ObjectID, start, end time.Time, statuses []domain.BookingStatus, limit int64) ([]domain.Booking, error)
}

// ExportBookings devuelve las reservas de los centros del usuario entre las
// fechas civiles startStr y endStr (yyyy-MM-dd, ambas inclusive).
func (uc *BookingUseCase) ExportBookings(ctx context.Context, userID, startStr, endStr, status string) ([]domain.Booking, error) {
	start, err := domain.ParseSantiagoDate(startStr)
	if err != nil {
		return nil, fmt.Errorf("%w: start debe ser yyyy-MM-dd", ErrInvalidExportRange)
	}
	end, err := domain.ParseSantiagoDate(endStr)
	if err != nil {
		return nil, fmt.Errorf("%w: end debe ser yyyy-MM-dd", ErrInvalidExportRange)
	}
	if end.Before(start) {
		return nil, fmt.Errorf("%w: la fecha final es anterior a la inicial", ErrInvalidExportRange)
	}
	if end.Sub(start) > time.Duration(MaxBookingExportDays-1)*24*time.Hour+12*time.Hour {
		return nil, fmt.Errorf("%w: el rango máximo es de %d días", ErrInvalidExportRange, MaxBookingExportDays)
	}

	var statuses []domain.BookingStatus
	switch domain.BookingStatus(status) {
	case "":
	case domain.BookingStatusConfirmed, domain.BookingStatusCancelled, domain.BookingStatusPending:
		statuses = []domain.BookingStatus{domain.BookingStatus(status)}
	default:
		return nil, fmt.Errorf("%w: estado desconocido", ErrInvalidExportRange)
	}

	store, ok := uc.repo.(BookingExportStore)
	if !ok {
		return nil, errors.New("exportación no disponible")
	}
	centers, err := uc.centerRepo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(centers) == 0 {
		return []domain.Booking{}, nil
	}
	ids := make([]primitive.ObjectID, len(centers))
	for i, c := range centers {
		ids[i] = c.ID
	}
	return store.FindForExport(ctx, ids, start, end, statuses, maxBookingExportRows)
}
