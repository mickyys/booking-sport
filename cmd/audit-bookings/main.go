// Command audit-bookings genera un inventario de sólo lectura sobre los
// documentos temporales de reservas y centros deportivos. No modifica datos.
//
// Uso:
//
//	MONGO_URI=mongodb://localhost:27017 go run ./cmd/audit-bookings
//	SAMPLE_LIMIT=20 OUTPUT_FILE=/tmp/audit-bookings.json go run ./cmd/audit-bookings
//
// Reporta:
//   - documentos sin local_date / scheduled_at / duration_minutes / timezone;
//   - fechas civiles incoherentes con el instante canónico;
//   - horas inexistentes o ambiguas en la zona del centro;
//   - solapamientos entre reservas activas (incluye cruce de medianoche);
//   - presencia de day_of_week en bookings;
//   - centros sin zona horaria.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type auditBooking struct {
	ID              primitive.ObjectID `bson:"_id"`
	CourtID         primitive.ObjectID `bson:"court_id"`
	SportCenterID   primitive.ObjectID `bson:"sport_center_id"`
	CourtName       string             `bson:"court_name"`
	Status          string             `bson:"status"`
	Date            *time.Time         `bson:"date"`
	Hour            int                `bson:"hour"`
	Minutes         int                `bson:"minutes"`
	DurationMinutes *int               `bson:"duration_minutes"`
	LocalDate       *string            `bson:"local_date"`
	Timezone        *string            `bson:"timezone"`
	ScheduledAt     *time.Time         `bson:"scheduled_at"`
	DayOfWeek       *int               `bson:"day_of_week"`
}

type interval struct {
	Booking auditBooking
	Start   time.Time
	End     time.Time
}

type finding struct {
	Kind       string `json:"kind"`
	BookingID  string `json:"booking_id,omitempty"`
	CourtID    string `json:"court_id,omitempty"`
	CenterID   string `json:"center_id,omitempty"`
	Status     string `json:"status,omitempty"`
	LocalDate  string `json:"local_date,omitempty"`
	Detail     string `json:"detail"`
	OtherID    string `json:"other_booking_id,omitempty"`
	OtherStart string `json:"other_start,omitempty"`
	OtherEnd   string `json:"other_end,omitempty"`
	CenterName string `json:"center_name,omitempty"`
}

type centerRecord struct {
	ID       primitive.ObjectID `bson:"_id"`
	Name     string             `bson:"name"`
	Slug     string             `bson:"slug"`
	Timezone *string            `bson:"timezone"`
}

type summary struct {
	Database            string         `json:"database"`
	GeneratedAt         string         `json:"generated_at"`
	TotalBookings       int            `json:"total_bookings"`
	ByStatus            map[string]int `json:"by_status"`
	MissingLocalDate    int            `json:"missing_local_date"`
	MissingScheduledAt  int            `json:"missing_scheduled_at"`
	MissingDuration     int            `json:"missing_duration_minutes"`
	MissingTimezone     int            `json:"missing_timezone"`
	InconsistentLocal   int            `json:"inconsistent_local_date"`
	InconsistentDate    int            `json:"inconsistent_date"`
	InvalidLocalTime    int            `json:"invalid_local_time"`
	OverlapPairs        int            `json:"overlap_pairs"`
	DayOfWeekDocuments  int            `json:"day_of_week_documents"`
	CentersTotal        int            `json:"centers_total"`
	CentersMissingTZ    int            `json:"centers_missing_timezone"`
	ActiveBookings      int            `json:"active_bookings"`
	Findings            []finding      `json:"findings"`
	UnverifiedTimezones []string       `json:"unverified_timezones,omitempty"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	mongoURI := env("MONGO_URI", "mongodb://localhost:27017")
	dbName := env("MONGO_DB", "sport_booking")
	sampleLimit := envInt("SAMPLE_LIMIT", 10)
	outputFile := os.Getenv("OUTPUT_FILE")
	markdownFile := os.Getenv("OUTPUT_MARKDOWN")

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("connect to MongoDB: %v", err)
	}
	defer client.Disconnect(context.Background())

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("ping MongoDB: %v", err)
	}
	db := client.Database(dbName)

	bookings, err := loadBookings(ctx, db)
	if err != nil {
		log.Fatalf("load bookings: %v", err)
	}

	report := summary{
		Database:    dbName,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		ByStatus:    map[string]int{},
	}

	for _, b := range bookings {
		report.TotalBookings++
		report.ByStatus[b.Status]++
		if isActive(b.Status) {
			report.ActiveBookings++
		}
	}

	report.Findings = append(report.Findings, auditTemporal(bookings, &report)...)
	report.Findings = append(report.Findings, auditOverlaps(bookings, &report)...)
	report.Findings = append(report.Findings, auditCenters(ctx, db, &report)...)
	report.Findings = append(report.Findings, auditDayOfWeek(ctx, db, &report)...)

	printReport(report, sampleLimit)

	if outputFile != "" {
		if err := writeJSON(outputFile, report); err != nil {
			log.Fatalf("write report: %v", err)
		}
		fmt.Printf("\nReporte JSON escrito en %s\n", outputFile)
	}

	if markdownFile != "" {
		if err := writeMarkdown(markdownFile, report, sampleLimit); err != nil {
			log.Fatalf("write markdown report: %v", err)
		}
		fmt.Printf("Inventario Markdown escrito en %s\n", markdownFile)
	}
}

func loadBookings(ctx context.Context, db *mongo.Database) ([]auditBooking, error) {
	cursor, err := db.Collection("bookings").Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var bookings []auditBooking
	if err := cursor.All(ctx, &bookings); err != nil {
		return nil, err
	}
	return bookings, nil
}

// auditTemporal revisa completitud y coherencia de los campos temporales.
func auditTemporal(bookings []auditBooking, report *summary) []finding {
	var findings []finding
	seenZones := map[string]bool{}

	for _, b := range bookings {
		id := b.ID.Hex()

		if b.LocalDate == nil || *b.LocalDate == "" {
			report.MissingLocalDate++
			findings = append(findings, finding{
				Kind: "missing_local_date", BookingID: id,
				CourtID: b.CourtID.Hex(), CenterID: b.SportCenterID.Hex(),
				Status: b.Status, Detail: "documento sin local_date",
			})
		}

		if b.ScheduledAt == nil || b.ScheduledAt.IsZero() {
			report.MissingScheduledAt++
			findings = append(findings, finding{
				Kind: "missing_scheduled_at", BookingID: id,
				CourtID: b.CourtID.Hex(), CenterID: b.SportCenterID.Hex(),
				Status: b.Status, Detail: "documento sin scheduled_at",
			})
		}

		if b.DurationMinutes == nil || *b.DurationMinutes <= 0 {
			report.MissingDuration++
			findings = append(findings, finding{
				Kind: "missing_duration_minutes", BookingID: id,
				CourtID: b.CourtID.Hex(), CenterID: b.SportCenterID.Hex(),
				Status: b.Status, Detail: "duración ausente o cero; se interpretará como 60 min",
			})
		}

		loc, zoneName, ok := zoneFor(b.Timezone)
		if !ok {
			report.MissingTimezone++
			findings = append(findings, finding{
				Kind: "missing_timezone", BookingID: id,
				CourtID: b.CourtID.Hex(), CenterID: b.SportCenterID.Hex(),
				Status: b.Status, Detail: "documento sin timezone válida",
			})
			continue
		}
		if !seenZones[zoneName] && zoneName != domain.SantiagoTimezone {
			seenZones[zoneName] = true
			report.UnverifiedTimezones = append(report.UnverifiedTimezones, zoneName)
		}

		start := startOf(b, loc)
		if start.IsZero() {
			continue
		}

		// Coherencia local_date vs instante.
		if b.LocalDate != nil && *b.LocalDate != "" {
			if want := start.In(loc).Format("2006-01-02"); want != *b.LocalDate {
				report.InconsistentLocal++
				findings = append(findings, finding{
					Kind: "inconsistent_local_date", BookingID: id,
					CourtID: b.CourtID.Hex(), CenterID: b.SportCenterID.Hex(),
					Status: b.Status, LocalDate: *b.LocalDate,
					Detail: fmt.Sprintf("local_date=%s pero el instante cae en %s", *b.LocalDate, want),
				})
			}
		}

		// Coherencia date (inicio del día civil) vs instante. Sólo se puede
		// verificar con los helpers actuales para America/Santiago.
		if b.Date != nil && zoneName == domain.SantiagoTimezone {
			wantDate, _ := domain.SantiagoDayBounds(start)
			if !sameInstant(*b.Date, wantDate) {
				report.InconsistentDate++
				findings = append(findings, finding{
					Kind: "inconsistent_date", BookingID: id,
					CourtID: b.CourtID.Hex(), CenterID: b.SportCenterID.Hex(),
					Status: b.Status,
					Detail: fmt.Sprintf("date=%s no es el inicio del día local (%s)",
						b.Date.UTC().Format(time.RFC3339), wantDate.UTC().Format(time.RFC3339)),
				})
			}
		}

		// Hora local inexistente o ambigua: sólo aplica a documentos legacy sin
		// scheduled_at, porque NormalizeSchedule reconstruye desde date+hour.
		if (b.ScheduledAt == nil || b.ScheduledAt.IsZero()) && b.Date != nil {
			if _, _, _, _, err := domain.NormalizeSchedule(*b.Date, b.Hour, b.Minutes); err != nil {
				var nonexistent *domain.NonexistentLocalTimeError
				var ambiguous *domain.AmbiguousLocalTimeError
				switch {
				case errors.As(err, &nonexistent):
					report.InvalidLocalTime++
					findings = append(findings, finding{
						Kind: "nonexistent_local_time", BookingID: id,
						CourtID: b.CourtID.Hex(), CenterID: b.SportCenterID.Hex(),
						Status: b.Status, Detail: err.Error(),
					})
				case errors.As(err, &ambiguous):
					report.InvalidLocalTime++
					findings = append(findings, finding{
						Kind: "ambiguous_local_time", BookingID: id,
						CourtID: b.CourtID.Hex(), CenterID: b.SportCenterID.Hex(),
						Status: b.Status, Detail: err.Error(),
					})
				}
			}
		}
	}

	return findings
}

// auditOverlaps detecta solapamientos entre reservas activas del mismo espacio.
// Compara por instante canónico, por lo que cubre cruces de medianoche.
func auditOverlaps(bookings []auditBooking, report *summary) []finding {
	byCourt := map[string][]interval{}

	for _, b := range bookings {
		if !isActive(b.Status) {
			continue
		}
		loc, _, ok := zoneFor(b.Timezone)
		if !ok {
			loc = domain.GetSantiagoLocation()
		}
		start := startOf(b, loc)
		if start.IsZero() {
			continue
		}
		duration := 60
		if b.DurationMinutes != nil && *b.DurationMinutes > 0 {
			duration = *b.DurationMinutes
		}
		byCourt[b.CourtID.Hex()] = append(byCourt[b.CourtID.Hex()], interval{
			Booking: b,
			Start:   start,
			End:     start.Add(time.Duration(duration) * time.Minute),
		})
	}

	var findings []finding

	for courtID, list := range byCourt {
		sort.Slice(list, func(i, j int) bool {
			if list[i].Start.Equal(list[j].Start) {
				return list[i].Booking.ID.Hex() < list[j].Booking.ID.Hex()
			}
			return list[i].Start.Before(list[j].Start)
		})

		var maxEnd time.Time
		var maxEndStart time.Time
		var maxEndBooking auditBooking

		for _, cur := range list {
			if !maxEnd.IsZero() && cur.Start.Before(maxEnd) {
				report.OverlapPairs++
				findings = append(findings, finding{
					Kind:      "overlap",
					BookingID: cur.Booking.ID.Hex(),
					CourtID:   courtID,
					CenterID:  cur.Booking.SportCenterID.Hex(),
					Status:    cur.Booking.Status,
					Detail: fmt.Sprintf("intervalo %s–%s se superpone con otra reserva activa",
						cur.Start.UTC().Format(time.RFC3339), cur.End.UTC().Format(time.RFC3339)),
					OtherID:    maxEndBooking.ID.Hex(),
					OtherStart: maxEndStart.UTC().Format(time.RFC3339),
					OtherEnd:   maxEnd.UTC().Format(time.RFC3339),
				})
			}
			if cur.End.After(maxEnd) {
				maxEnd = cur.End
				maxEndStart = cur.Start
				maxEndBooking = cur.Booking
			}
		}
	}

	return findings
}

func auditCenters(ctx context.Context, db *mongo.Database, report *summary) []finding {
	cursor, err := db.Collection("sport_centers").Find(ctx, bson.M{})
	if err != nil {
		log.Printf("warn: no se pudieron leer sport_centers: %v", err)
		return nil
	}
	defer cursor.Close(ctx)

	var centers []centerRecord
	if err := cursor.All(ctx, &centers); err != nil {
		log.Printf("warn: no se pudieron decodificar sport_centers: %v", err)
		return nil
	}

	report.CentersTotal = len(centers)
	var findings []finding

	for _, c := range centers {
		if c.Timezone == nil || *c.Timezone == "" {
			report.CentersMissingTZ++
			findings = append(findings, finding{
				Kind:       "center_missing_timezone",
				CenterID:   c.ID.Hex(),
				CenterName: c.Name,
				Detail:     fmt.Sprintf("centro %q (%s) sin zona horaria configurada", c.Name, c.Slug),
			})
			continue
		}
		if _, err := time.LoadLocation(*c.Timezone); err != nil {
			findings = append(findings, finding{
				Kind:       "center_invalid_timezone",
				CenterID:   c.ID.Hex(),
				CenterName: c.Name,
				Detail:     fmt.Sprintf("zona %q inválida: %v", *c.Timezone, err),
			})
		}
	}

	return findings
}

func auditDayOfWeek(ctx context.Context, db *mongo.Database, report *summary) []finding {
	count, err := db.Collection("bookings").CountDocuments(ctx, bson.M{"day_of_week": bson.M{"$exists": true}})
	if err != nil {
		log.Printf("warn: no se pudo contar day_of_week: %v", err)
		return nil
	}
	report.DayOfWeekDocuments = int(count)
	if count == 0 {
		return nil
	}
	return []finding{{
		Kind:   "day_of_week_present",
		Detail: fmt.Sprintf("%d reservas tienen el campo legacy day_of_week; revisar la consulta FindBySportCenterAndDate", count),
	}}
}

func printReport(report summary, sampleLimit int) {
	fmt.Println("=== Auditoría de reservas (booking-sport) ===")
	fmt.Printf("Base de datos:          %s\n", report.Database)
	fmt.Printf("Generado:               %s\n", report.GeneratedAt)
	fmt.Printf("Reservas totales:       %d\n", report.TotalBookings)
	fmt.Printf("Reservas activas:       %d (confirmed + pending)\n", report.ActiveBookings)
	fmt.Println("Por estado:")
	statuses := make([]string, 0, len(report.ByStatus))
	for s := range report.ByStatus {
		statuses = append(statuses, s)
	}
	sort.Strings(statuses)
	for _, s := range statuses {
		fmt.Printf("  %-10s %d\n", s, report.ByStatus[s])
	}

	fmt.Println("\nCompletitud temporal:")
	fmt.Printf("  sin local_date:          %d\n", report.MissingLocalDate)
	fmt.Printf("  sin scheduled_at:        %d\n", report.MissingScheduledAt)
	fmt.Printf("  sin duration_minutes:    %d\n", report.MissingDuration)
	fmt.Printf("  sin timezone:            %d\n", report.MissingTimezone)
	fmt.Printf("  local_date incoherente:  %d\n", report.InconsistentLocal)
	fmt.Printf("  date incoherente:        %d\n", report.InconsistentDate)
	fmt.Printf("  hora inválida/ambigua:   %d\n", report.InvalidLocalTime)

	fmt.Println("\nIntegridad:")
	fmt.Printf("  pares solapados:         %d\n", report.OverlapPairs)
	fmt.Printf("  docs con day_of_week:    %d\n", report.DayOfWeekDocuments)
	fmt.Printf("  centros totales:         %d\n", report.CentersTotal)
	fmt.Printf("  centros sin timezone:    %d\n", report.CentersMissingTZ)
	if len(report.UnverifiedTimezones) > 0 {
		fmt.Printf("  zonas no Santiago:       %v\n", report.UnverifiedTimezones)
	}

	fmt.Println("\nTotal de hallazgos:", len(report.Findings))
	for i, f := range report.Findings {
		if i >= sampleLimit {
			fmt.Printf("  ... y %d hallazgo(s) más (usa SAMPLE_LIMIT u OUTPUT_FILE)\n", len(report.Findings)-sampleLimit)
			break
		}
		fmt.Printf("  - [%s] %s\n", f.Kind, f.Detail)
	}
}

func startOf(b auditBooking, loc *time.Location) time.Time {
	if b.ScheduledAt != nil && !b.ScheduledAt.IsZero() {
		return b.ScheduledAt.UTC()
	}
	if b.Date != nil {
		d := b.Date.In(loc)
		return time.Date(d.Year(), d.Month(), d.Day(), b.Hour, b.Minutes, 0, 0, loc).UTC()
	}
	return time.Time{}
}

func zoneFor(tz *string) (*time.Location, string, bool) {
	if tz == nil || *tz == "" {
		return nil, "", false
	}
	loc, err := time.LoadLocation(*tz)
	if err != nil {
		return nil, *tz, false
	}
	return loc, *tz, true
}

func isActive(status string) bool {
	return status == string(domain.BookingStatusConfirmed) || status == string(domain.BookingStatusPending)
}

func sameInstant(a, b time.Time) bool {
	return a.UTC().Truncate(time.Second).Equal(b.UTC().Truncate(time.Second))
}

func writeJSON(path string, report summary) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// writeMarkdown escribe el inventario en Markdown, listo para versionar en
// docs/migration-inventory.md.
func writeMarkdown(path string, report summary, sampleLimit int) error {
	var b strings.Builder

	fmt.Fprintf(&b, "# Inventario de migración — reservas UTC\n\n")
	fmt.Fprintf(&b, "- **Base de datos:** `%s`\n", report.Database)
	fmt.Fprintf(&b, "- **Generado:** %s\n\n", report.GeneratedAt)

	fmt.Fprintf(&b, "## Volumen\n\n")
	fmt.Fprintf(&b, "| Métrica | Valor |\n|---|---|\n")
	fmt.Fprintf(&b, "| Reservas totales | %d |\n", report.TotalBookings)
	fmt.Fprintf(&b, "| Reservas activas (confirmed + pending) | %d |\n", report.ActiveBookings)
	fmt.Fprintf(&b, "| Centros | %d |\n\n", report.CentersTotal)

	if len(report.ByStatus) > 0 {
		fmt.Fprintf(&b, "## Por estado\n\n| Estado | Cantidad |\n|---|---|\n")
		statuses := make([]string, 0, len(report.ByStatus))
		for s := range report.ByStatus {
			statuses = append(statuses, s)
		}
		sort.Strings(statuses)
		for _, s := range statuses {
			fmt.Fprintf(&b, "| `%s` | %d |\n", s, report.ByStatus[s])
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "## Completitud temporal\n\n| Hallazgo | Documentos |\n|---|---|\n")
	fmt.Fprintf(&b, "| Sin `local_date` | %d |\n", report.MissingLocalDate)
	fmt.Fprintf(&b, "| Sin `scheduled_at` | %d |\n", report.MissingScheduledAt)
	fmt.Fprintf(&b, "| Sin `duration_minutes` | %d |\n", report.MissingDuration)
	fmt.Fprintf(&b, "| Sin `timezone` válida | %d |\n", report.MissingTimezone)
	fmt.Fprintf(&b, "| `local_date` incoherente | %d |\n", report.InconsistentLocal)
	fmt.Fprintf(&b, "| `date` incoherente | %d |\n", report.InconsistentDate)
	fmt.Fprintf(&b, "| Hora inexistente o ambigua | %d |\n\n", report.InvalidLocalTime)

	fmt.Fprintf(&b, "## Integridad\n\n| Hallazgo | Documentos |\n|---|---|\n")
	fmt.Fprintf(&b, "| Pares solapados | %d |\n", report.OverlapPairs)
	fmt.Fprintf(&b, "| Documentos con `day_of_week` | %d |\n", report.DayOfWeekDocuments)
	fmt.Fprintf(&b, "| Centros sin `timezone` | %d |\n\n", report.CentersMissingTZ)

	if len(report.UnverifiedTimezones) > 0 {
		fmt.Fprintf(&b, "Zonas distintas de `America/Santiago` detectadas: %s.\n\n",
			strings.Join(report.UnverifiedTimezones, ", "))
	}

	fmt.Fprintf(&b, "## Hallazgos (%d)\n\n", len(report.Findings))
	if len(report.Findings) == 0 {
		b.WriteString("Sin hallazgos.\n")
	} else {
		b.WriteString("| Tipo | Reserva | Espacio | Estado | Detalle |\n|---|---|---|---|---|\n")
		for i, f := range report.Findings {
			if i >= sampleLimit {
				fmt.Fprintf(&b, "\n_… y %d hallazgo(s) más. Ejecutar con `SAMPLE_LIMIT` mayor o revisar el JSON._\n",
					len(report.Findings)-sampleLimit)
				break
			}
			fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | `%s` | %s |\n",
				f.Kind, shortID(f.BookingID), shortID(f.CourtID), f.Status, escapePipes(f.Detail))
		}
	}

	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func escapePipes(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}
