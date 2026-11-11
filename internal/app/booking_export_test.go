package app

import (
	"context"
	"errors"
	"testing"
)

func TestExportBookingsRejectsInvalidRange(t *testing.T) {
	uc := &BookingUseCase{}
	cases := map[string][2]string{
		"formato":   {"2026-13-01", "2026-01-10"},
		"orden":     {"2026-02-10", "2026-02-01"},
		"muy largo": {"2026-01-01", "2026-06-01"},
	}
	for name, c := range cases {
		_, err := uc.ExportBookings(context.Background(), "u1", c[0], c[1], "")
		if !errors.Is(err, ErrInvalidExportRange) {
			t.Fatalf("%s: se esperaba ErrInvalidExportRange, llegó %v", name, err)
		}
	}
}

func TestExportBookingsRejectsUnknownStatus(t *testing.T) {
	uc := &BookingUseCase{}
	_, err := uc.ExportBookings(context.Background(), "u1", "2026-01-01", "2026-01-31", "nope")
	if !errors.Is(err, ErrInvalidExportRange) {
		t.Fatalf("se esperaba ErrInvalidExportRange, llegó %v", err)
	}
}

func TestExportBookingsAcceptsMaxRange(t *testing.T) {
	uc := &BookingUseCase{}
	// 92 días inclusive: pasa la validación y falla recién al no haber repo.
	defer func() { _ = recover() }()
	_, err := uc.ExportBookings(context.Background(), "u1", "2026-01-01", "2026-04-02", "")
	if errors.Is(err, ErrInvalidExportRange) {
		t.Fatalf("92 días debería ser válido: %v", err)
	}
}
