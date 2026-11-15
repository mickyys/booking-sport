package infra

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"github.com/xuri/excelize/v2"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type accessBulkInput struct {
	IDs    []string `json:"ids"`
	Filter *struct {
		Status string `json:"status"`
		Query  string `json:"q"`
		From   string `json:"from"`
		To     string `json:"to"`
	} `json:"filter"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// BulkSetStatus aprueba, rechaza o revoca varias solicitudes a la vez, ya sea
// por ids o por todo lo que coincide con un filtro.
func (h *AccessHandler) BulkSetStatus(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	var body accessBulkInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "datos incompletos"})
		return
	}
	var ids []primitive.ObjectID
	var filter *app.CenterAccessRequestFilter
	if body.Filter != nil {
		f := app.CenterAccessRequestFilter{
			Status: strings.TrimSpace(body.Filter.Status),
			Query:  strings.TrimSpace(body.Filter.Query),
		}
		loc := domain.GetSantiagoLocation()
		if from, err := time.ParseInLocation("2006-01-02", body.Filter.From, loc); err == nil {
			f.From = &from
		}
		if to, err := time.ParseInLocation("2006-01-02", body.Filter.To, loc); err == nil {
			end := to.AddDate(0, 0, 1)
			f.To = &end
		}
		filter = &f
	} else {
		for _, raw := range body.IDs {
			id, err := primitive.ObjectIDFromHex(raw)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
				return
			}
			ids = append(ids, id)
		}
	}
	actorID := h.baseHandler.GetUserID(c)
	actorEmail := c.GetString("user_email")
	result, err := h.useCase.BulkSetStatus(c.Request.Context(), center.ID, ids, filter, body.Status, body.Reason, actorID, actorEmail)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	status := strings.ToLower(strings.TrimSpace(body.Status))
	for _, raw := range result.UpdatedIDs {
		if id, err := primitive.ObjectIDFromHex(raw); err == nil {
			h.writeAudit(c, "center_access."+status, center.ID, id, strings.TrimSpace(body.Reason), map[string]interface{}{"status": status, "bulk": true})
		}
	}
	c.JSON(http.StatusOK, result)
}

// ResendApproval reenvía el correo de aprobación.
func (h *AccessHandler) ResendApproval(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return
	}
	if err := h.useCase.ResendApproval(c.Request.Context(), center.ID, id); err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ExportRequests descarga en XLSX las solicitudes que coinciden con el filtro.
func (h *AccessHandler) ExportRequests(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	filter := accessFilterFromQuery(c, center.ID)
	filter.All = true
	items, _, err := h.useCase.ListRequests(c.Request.Context(), filter)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	form, _ := h.useCase.GetForm(c.Request.Context(), center.ID)
	file, err := buildAccessWorkbook(items, form)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo generar el reporte"})
		return
	}
	defer file.Close()

	name := fmt.Sprintf("solicitudes_acceso_%s.xlsx", time.Now().In(domain.GetSantiagoLocation()).Format("2006-01-02"))
	c.Header("Content-Type", xlsxContentType)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	_ = file.Write(c.Writer)
}

var accessStatusLabel = map[string]string{
	domain.CenterAccessStatusPending:  "Pendiente",
	domain.CenterAccessStatusApproved: "Aprobada",
	domain.CenterAccessStatusRejected: "Rechazada",
	domain.CenterAccessStatusRevoked:  "Revocada",
}

func accessDateCell(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.In(domain.GetSantiagoLocation()).Format("2006-01-02 15:04")
}

// formatAccessRUT devuelve el RUT con puntos y guion (12.345.678-9).
func formatAccessRUT(rut string) string {
	rut = strings.ToUpper(strings.NewReplacer(".", "", "-", "", " ", "").Replace(rut))
	if len(rut) < 2 {
		return rut
	}
	body, dv := rut[:len(rut)-1], rut[len(rut)-1:]
	var parts []string
	for len(body) > 3 {
		parts = append([]string{body[len(body)-3:]}, parts...)
		body = body[:len(body)-3]
	}
	parts = append([]string{body}, parts...)
	return strings.Join(parts, ".") + "-" + dv
}

func buildAccessWorkbook(items []domain.CenterAccessRequest, form *domain.CenterAccessForm) (*excelize.File, error) {
	f := excelize.NewFile()
	const sheet = "Solicitudes"
	f.SetSheetName("Sheet1", sheet)

	headers := []string{"Nombre", "RUT", "Correo", "Estado", "Origen", "Fecha de solicitud", "Decidido por", "Fecha de decisión", "Motivo"}
	var custom []domain.CenterAccessField
	if form != nil {
		for _, field := range form.Fields {
			if !field.Locked {
				custom = append(custom, field)
				headers = append(headers, field.Label)
			}
		}
	}
	for i, hd := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, hd)
	}
	if style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}}); err == nil {
		last, _ := excelize.CoordinatesToCellName(len(headers), 1)
		_ = f.SetCellStyle(sheet, "A1", last, style)
	}

	for i, r := range items {
		source := "Formulario"
		if r.Source == domain.CenterAccessSourceAdmin {
			source = "Pre-registro"
		}
		created := r.CreatedAt
		row := []interface{}{
			r.Name, formatAccessRUT(r.RUT), r.Email, accessStatusLabel[r.Status], source,
			accessDateCell(&created), r.DecidedByEmail, accessDateCell(r.DecidedAt), r.Reason,
		}
		answers := map[string]string{}
		for _, a := range r.Answers {
			answers[a.Key] = a.Value
		}
		for _, field := range custom {
			row = append(row, answers[field.Key])
		}
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		_ = f.SetSheetRow(sheet, cell, &row)
	}
	for i := range headers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		w := 22.0
		if i >= 8 {
			w = 28
		}
		_ = f.SetColWidth(sheet, col, col, w)
	}
	_ = f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	return f, nil
}

// ImportTemplate entrega la plantilla XLSX para la importación masiva.
func (h *AccessHandler) ImportTemplate(c *gin.Context) {
	if _, ok := h.adminCenter(c); !ok {
		return
	}
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Personas"
	f.SetSheetName("Sheet1", sheet)
	_ = f.SetSheetRow(sheet, "A1", &[]interface{}{"RUT", "Correo", "Nombre"})
	_ = f.SetSheetRow(sheet, "A2", &[]interface{}{"12.345.678-5", "persona@correo.cl", "Nombre Apellido"})
	_ = f.SetColWidth(sheet, "A", "C", 28)
	c.Header("Content-Type", xlsxContentType)
	c.Header("Content-Disposition", `attachment; filename="plantilla_acceso.xlsx"`)
	c.Status(http.StatusOK)
	_ = f.Write(c.Writer)
}

// ImportRequests recibe un XLSX o CSV (RUT, Correo, Nombre). Con ?dry_run=1
// solo valida y devuelve el resumen.
func (h *AccessHandler) ImportRequests(c *gin.Context) {
	center, ok := h.adminCenter(c)
	if !ok {
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "adjunta un archivo"})
		return
	}
	if fh.Size > 2<<20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "el archivo no puede superar 2 MB"})
		return
	}
	src, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no se pudo leer el archivo"})
		return
	}
	defer src.Close()
	data, err := io.ReadAll(src)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no se pudo leer el archivo"})
		return
	}
	records, err := readAccessImportRecords(strings.ToLower(filepath.Ext(fh.Filename)), data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rows := make([]app.AccessImportRow, 0, len(records))
	for i, rec := range records {
		cell := func(n int) string {
			if n < len(rec) {
				return strings.TrimSpace(rec[n])
			}
			return ""
		}
		if i == 0 && strings.Contains(strings.ToLower(cell(0)), "rut") {
			continue
		}
		if cell(0) == "" && cell(1) == "" && cell(2) == "" {
			continue
		}
		rows = append(rows, app.AccessImportRow{Line: i + 1, RUT: cell(0), Email: cell(1), Name: cell(2)})
	}

	dry := c.Query("dry_run") == "1"
	actorID := h.baseHandler.GetUserID(c)
	actorEmail := c.GetString("user_email")
	result, err := h.useCase.ImportRequests(c.Request.Context(), center.ID, actorID, actorEmail, rows, dry)
	if err != nil {
		c.JSON(errorStatusCode(err), gin.H{"error": err.Error()})
		return
	}
	if !dry && result.Imported > 0 {
		h.writeAudit(c, "center_access.import", center.ID, primitive.NilObjectID, "importación masiva", map[string]interface{}{"imported": result.Imported})
	}
	c.JSON(http.StatusOK, result)
}

func readAccessImportRecords(ext string, data []byte) ([][]string, error) {
	if ext == ".xlsx" {
		f, err := excelize.OpenReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("el archivo XLSX no es válido")
		}
		defer f.Close()
		sheets := f.GetSheetList()
		if len(sheets) == 0 {
			return nil, fmt.Errorf("el archivo no tiene hojas")
		}
		rows, err := f.GetRows(sheets[0])
		if err != nil {
			return nil, fmt.Errorf("no se pudo leer la hoja")
		}
		return rows, nil
	}
	if ext != ".csv" {
		return nil, fmt.Errorf("el archivo debe ser .xlsx o .csv")
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	reader := csv.NewReader(strings.NewReader(text))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	if first, _, _ := strings.Cut(text, "\n"); strings.Count(first, ";") > strings.Count(first, ",") {
		reader.Comma = ';'
	}
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("el archivo CSV no es válido")
	}
	return records, nil
}
