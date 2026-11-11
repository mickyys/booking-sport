package infra

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"github.com/xuri/excelize/v2"
)

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// ExportBookings descarga en XLSX las reservas de los centros del admin entre
// dos fechas civiles (start y end, yyyy-MM-dd, inclusive).
func (h *BookingHandler) ExportBookings(c *gin.Context) {
	userID, exists := c.Get("user_id")
	userIDStr, ok := userID.(string)
	if !exists || !ok || userIDStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found in token"})
		return
	}

	start := c.Query("start")
	end := c.Query("end")
	bookings, err := h.useCase.ExportBookings(c.Request.Context(), userIDStr, start, end, c.Query("status"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, app.ErrInvalidExportRange) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	file, err := buildBookingsWorkbook(bookings)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo generar el reporte"})
		return
	}
	defer file.Close()

	c.Header("Content-Type", xlsxContentType)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="reservas_%s_%s.xlsx"`, start, end))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	_ = file.Write(c.Writer)
}

var bookingExportHeaders = []string{
	"Código", "Espacio", "Fecha", "Hora", "Duración (min)", "Estado", "Origen",
	"Método de pago", "Precio", "Pagado", "Pendiente", "Cliente", "Teléfono", "Correo",
}

func buildBookingsWorkbook(bookings []domain.Booking) (*excelize.File, error) {
	f := excelize.NewFile()
	const sheet = "Reservas"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}

	for i, title := range bookingExportHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, title)
	}
	if style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}}); err == nil {
		_ = f.SetCellStyle(sheet, "A1", "N1", style)
	}

	for i := range bookings {
		b := &bookings[i]
		name, phone, email := exportCustomer(b)
		row := []interface{}{
			exportText(b.BookingCode),
			exportText(b.CourtName),
			exportDate(b),
			fmt.Sprintf("%02d:%02d", b.Hour, b.Minutes),
			b.DurationOrDefault(),
			exportStatus(b.Status),
			exportOrigin(b),
			exportPaymentMethod(b.PaymentMethod),
			b.ChargedPrice(),
			b.PaidAmount,
			b.PendingAmount,
			exportText(name),
			exportText(phone),
			exportText(email),
		}
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		if err := f.SetSheetRow(sheet, cell, &row); err != nil {
			return nil, err
		}
	}

	widths := []float64{16, 22, 12, 8, 14, 12, 12, 16, 12, 12, 12, 24, 16, 28}
	for i, w := range widths {
		col, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(sheet, col, col, w)
	}
	_ = f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	return f, nil
}

func exportDate(b *domain.Booking) string {
	if b.LocalDate != "" {
		return b.LocalDate
	}
	return b.Date.In(domain.GetSantiagoLocation()).Format("2006-01-02")
}

func exportCustomer(b *domain.Booking) (name, phone, email string) {
	name, phone = b.CustomerName, b.CustomerPhone
	if b.GuestDetails != nil {
		if name == "" {
			name = b.GuestDetails.Name
		}
		if phone == "" {
			phone = b.GuestDetails.Phone
		}
		email = b.GuestDetails.Email
	}
	return name, phone, email
}

func exportStatus(s domain.BookingStatus) string {
	switch s {
	case domain.BookingStatusConfirmed:
		return "Confirmada"
	case domain.BookingStatusPending:
		return "Pendiente"
	case domain.BookingStatusCancelled:
		return "Cancelada"
	default:
		return string(s)
	}
}

func exportOrigin(b *domain.Booking) string {
	switch {
	case b.RecurringID != "":
		return "Semanal"
	case b.SeriesID != "":
		return "Serie"
	}
	switch b.PaymentMethod {
	case "internal", "presential", "venue":
		return "Manual"
	}
	return "Online"
}

func exportPaymentMethod(m string) string {
	switch m {
	case "internal", "presential", "venue":
		return "En el recinto"
	case "mercadopago":
		return "Mercado Pago"
	default:
		return m
	}
}

// exportText evita que una hoja de cálculo interprete el texto como fórmula.
func exportText(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
