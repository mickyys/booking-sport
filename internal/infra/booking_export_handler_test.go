package infra

import (
	"testing"

	"github.com/hamp/booking-sport/internal/domain"
)

func TestBuildBookingsWorkbook(t *testing.T) {
	bookings := []domain.Booking{
		{
			BookingCode: "ABC123", CourtName: "Cancha 1", LocalDate: "2026-03-05",
			Hour: 9, Minutes: 30, Status: domain.BookingStatusConfirmed,
			PaymentMethod: "mercadopago", Price: 20000, PaidAmount: 20000,
			GuestDetails: &domain.GuestDetails{Name: "=cmd", Email: "a@b.cl", Phone: "+56911111111"},
		},
		{
			BookingCode: "XYZ", CourtName: "Cancha 2", LocalDate: "2026-03-06",
			Status: domain.BookingStatusCancelled, RecurringID: "r1", CustomerName: "Ana",
		},
	}
	f, err := buildBookingsWorkbook(bookings)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	get := func(cell string) string {
		v, _ := f.GetCellValue("Reservas", cell)
		return v
	}
	if get("A1") != "Código" || get("A2") != "ABC123" {
		t.Fatalf("cabecera/código incorrectos: %q %q", get("A1"), get("A2"))
	}
	if get("D2") != "09:30" || get("F2") != "Confirmada" || get("G2") != "Online" {
		t.Fatalf("fila 2 incorrecta: %q %q %q", get("D2"), get("F2"), get("G2"))
	}
	if get("L2") != "'=cmd" {
		t.Fatalf("el nombre debe neutralizar fórmulas, llegó %q", get("L2"))
	}
	if get("F3") != "Cancelada" || get("G3") != "Semanal" || get("L3") != "Ana" {
		t.Fatalf("fila 3 incorrecta: %q %q %q", get("F3"), get("G3"), get("L3"))
	}
}
