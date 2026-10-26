// Command migrate-recurring-bookings genera las primeras ocurrencias (4
// semanas) de todas las reservas recurrentes activas como parte de la
// migración, con control y verificación. Usa el mismo generador que el job
// diario (POST /api/internal/jobs/recurring-generate), por lo que las reservas
// quedan vinculadas con `recurring_id` y respetan horarios, bloqueos y
// conflictos. Es idempotente: una segunda ejecución no crea nada.
//
// Debe ejecutarse al final de la Fase 1 (después de migrate-schedule-rules,
// migrate-quincho-day y migrate-booking-schedule-fields) y con el backend
// apuntando a la misma base.
//
// DRY_RUN por defecto: solo lee la base y muestra, por recurrente, las fechas
// que se generarían y los conflictos conocidos con reservas confirmadas
// (las reglas de horario las valida el generador al aplicar).
//
//	MONGO_URI=... go run ./cmd/migrate-recurring-bookings
//
// Aplicar (backend en API_BASE_URL con RECURRING_JOB_TOKEN configurado):
//
//	APPLY=true API_BASE_URL=http://localhost:8080 RECURRING_JOB_TOKEN=... \
//	  MONGO_URI=... go run ./cmd/migrate-recurring-bookings
//
// Revertir lo generado desde un instante (RFC3339, el que imprime el apply):
//
//	ROLLBACK=true APPLY=true SINCE=2026-10-25T20:00:00Z MONGO_URI=... go run ./cmd/migrate-recurring-bookings
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/migratelog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const horizonWeeks = 4

type summary struct {
	Scanned       int  `json:"scanned"`
	Created       int  `json:"created"`
	AlreadyExists int  `json:"already_exists"`
	Skipped       int  `json:"skipped"`
	Failed        int  `json:"failed"`
	SeriesErrors  int  `json:"series_errors"`
	Locked        bool `json:"locked"`
}

func main() {
	defer migratelog.Start("migrate-recurring-bookings")()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	mongoURI := env("MONGO_URI", "mongodb://localhost:27017")
	dbName := env("MONGO_DB", "sport_booking")
	apply := env("APPLY", "false") == "true"

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database(dbName)

	if env("ROLLBACK", "false") == "true" {
		rollback(ctx, db, apply)
		return
	}

	mode := "DRY_RUN"
	if apply {
		mode = "APPLY"
	}
	log.Printf("[RECURRING] mode=%s db=%s", mode, dbName)

	before := countGenerated(ctx, db)
	plan := analyze(ctx, db)
	log.Printf("[RECURRING] ya generadas con recurring_id=%d", before)
	if !apply {
		log.Printf("[RECURRING] DRY_RUN: no se escribió nada. Repetir con APPLY=true.")
		return
	}

	token := os.Getenv("RECURRING_JOB_TOKEN")
	if token == "" {
		log.Fatalf("RECURRING_JOB_TOKEN es obligatorio para APPLY")
	}
	baseURL := strings.TrimRight(env("API_BASE_URL", "http://localhost:8080"), "/")

	started := time.Now().UTC()
	log.Printf("[RECURRING] inicio=%s (usar con SINCE para revertir)", started.Format(time.RFC3339))
	first := callGenerator(ctx, baseURL, token)
	log.Printf("[RECURRING] 1ª corrida: %+v", first)
	second := callGenerator(ctx, baseURL, token)
	log.Printf("[RECURRING] 2ª corrida: %+v", second)

	after := countGenerated(ctx, db)
	failures := verify(ctx, db, plan, first, second, before, after)
	if failures > 0 {
		log.Fatalf("[RECURRING] verificación con %d problema(s)", failures)
	}
	log.Printf("[RECURRING] OK: generadas=%d (antes %d)", after-before, before)
}

type seriesPlan struct {
	ID       primitive.ObjectID
	Name     string
	Court    primitive.ObjectID
	DOW      int
	Hour     int
	Minutes  int
	Dates    []string
	Conflict []string
}

func analyze(ctx context.Context, db *mongo.Database) []seriesPlan {
	cur, err := db.Collection("recurring_reservations").Find(ctx, bson.M{"status": "active"})
	if err != nil {
		log.Fatalf("find recurring: %v", err)
	}
	var series []domain.RecurringReservation
	if err := cur.All(ctx, &series); err != nil {
		log.Fatalf("decode recurring: %v", err)
	}

	loc := domain.GetSantiagoLocation()
	nowLoc := time.Now().In(loc)
	today := domain.SantiagoCivilDate(nowLoc)
	plans := make([]seriesPlan, 0, len(series))
	conflicts := 0
	totalDates := 0
	for _, r := range series {
		p := seriesPlan{ID: r.ID, Name: r.CustomerName, Court: r.CourtID, DOW: r.DayOfWeek, Hour: r.Hour, Minutes: r.Minutes}
		base := today
		if s := r.EffectiveStartDate(); !s.IsZero() && s.After(base) {
			base = s
		}
		delta := (r.DayOfWeek - int(base.Weekday()) + 7) % 7
		first := base.AddDate(0, 0, delta)
		slot := time.Date(first.Year(), first.Month(), first.Day(), r.Hour, r.Minutes, 0, 0, loc)
		if !slot.After(nowLoc) {
			first = first.AddDate(0, 0, 7)
		}
		for i := 0; i < horizonWeeks; i++ {
			d := first.AddDate(0, 0, 7*i)
			if r.IsDateCancelled(d) {
				continue
			}
			ds := d.Format("2006-01-02")
			p.Dates = append(p.Dates, ds)
			n, err := db.Collection("bookings").CountDocuments(ctx, bson.M{
				"court_id": r.CourtID, "local_date": ds, "hour": r.Hour, "minutes": r.Minutes,
				"status": bson.M{"$in": bson.A{"confirmed", "pending"}},
			})
			if err != nil {
				log.Fatalf("count: %v", err)
			}
			if n > 0 {
				p.Conflict = append(p.Conflict, ds)
			}
		}
		totalDates += len(p.Dates)
		conflicts += len(p.Conflict)
		if len(p.Conflict) > 0 {
			log.Printf("[RECURRING] %s %-28s dow=%d %02d:%02d conflicto con reserva existente en %v", r.ID.Hex(), r.CustomerName, r.DayOfWeek, r.Hour, r.Minutes, p.Conflict)
		}
		plans = append(plans, p)
	}
	log.Printf("[RECURRING] recurrentes activas=%d fechas a generar=%d conflictos conocidos=%d", len(plans), totalDates, conflicts)
	return plans
}

func callGenerator(ctx context.Context, baseURL, token string) summary {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/internal/jobs/recurring-generate", bytes.NewReader([]byte("{}")))
	if err != nil {
		log.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Job-Token", token)
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		log.Fatalf("llamando al generador: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("el generador respondió %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var s summary
	if err := json.Unmarshal(body, &s); err != nil {
		log.Fatalf("respuesta inválida: %v (%s)", err, body)
	}
	if s.Locked {
		log.Fatalf("el generador está bloqueado por otra ejecución; reintentar")
	}
	return s
}

func countGenerated(ctx context.Context, db *mongo.Database) int64 {
	n, err := db.Collection("bookings").CountDocuments(ctx, bson.M{"recurring_id": bson.M{"$exists": true}})
	if err != nil {
		log.Fatalf("count generated: %v", err)
	}
	return n
}

func verify(ctx context.Context, db *mongo.Database, plans []seriesPlan, first, second summary, before, after int64) int {
	problems := 0
	fail := func(format string, a ...interface{}) {
		problems++
		log.Printf("[RECURRING] PROBLEMA: "+format, a...)
	}

	if first.Failed > 0 || first.SeriesErrors > 0 {
		fail("el generador reportó failed=%d series_errors=%d", first.Failed, first.SeriesErrors)
	}
	if second.Created != 0 {
		fail("la segunda corrida creó %d reserva(s): no es idempotente", second.Created)
	}
	if int64(first.Created) != after-before {
		fail("creadas según el generador (%d) distinto de las reservas con recurring_id nuevas (%d)", first.Created, after-before)
	}

	dups, err := db.Collection("bookings").Aggregate(ctx, bson.A{
		bson.M{"$match": bson.M{"status": "confirmed"}},
		bson.M{"$group": bson.M{"_id": bson.M{"c": "$court_id", "d": "$local_date", "h": "$hour", "m": "$minutes"}, "n": bson.M{"$sum": 1}}},
		bson.M{"$match": bson.M{"n": bson.M{"$gt": 1}}},
	})
	if err != nil {
		log.Fatalf("duplicados: %v", err)
	}
	var d []bson.M
	_ = dups.All(ctx, &d)
	if len(d) > 0 {
		fail("hay %d horario(s) con más de una reserva confirmada", len(d))
	}

	// Series que no quedaron con sus fechas: se informan con el motivo guardado.
	incomplete := 0
	for _, p := range plans {
		var got int64
		got, _ = db.Collection("bookings").CountDocuments(ctx, bson.M{"recurring_id": p.ID.Hex(), "status": "confirmed", "local_date": bson.M{"$in": p.Dates}})
		if int(got) == len(p.Dates) {
			continue
		}
		incomplete++
		var rr struct {
			Issues []domain.RecurringGenerationIssue `bson:"generation_issues"`
		}
		_ = db.Collection("recurring_reservations").FindOne(ctx, bson.M{"_id": p.ID}).Decode(&rr)
		reasons := make([]string, 0, len(rr.Issues))
		for _, is := range rr.Issues {
			reasons = append(reasons, is.Date+": "+is.Reason)
		}
		log.Printf("[RECURRING] incompleta %s %-28s esperadas=%d generadas=%d motivos=%s", p.ID.Hex(), p.Name, len(p.Dates), got, strings.Join(reasons, "; "))
	}
	if incomplete > 0 {
		fail("%d serie(s) sin todas sus ocurrencias (revisar motivos)", incomplete)
	}
	return problems
}

func rollback(ctx context.Context, db *mongo.Database, apply bool) {
	since := os.Getenv("SINCE")
	t, err := time.Parse(time.RFC3339, since)
	if err != nil {
		log.Fatalf("SINCE (RFC3339) es obligatorio para ROLLBACK: %v", err)
	}
	filter := bson.M{"recurring_id": bson.M{"$exists": true}, "created_at": bson.M{"$gte": t}, "payment_method": "internal"}
	n, err := db.Collection("bookings").CountDocuments(ctx, filter)
	if err != nil {
		log.Fatalf("count: %v", err)
	}
	log.Printf("[RECURRING] ROLLBACK: %d reserva(s) generadas desde %s", n, t.Format(time.RFC3339))
	if !apply {
		log.Printf("[RECURRING] DRY_RUN: repetir con APPLY=true para borrarlas.")
		return
	}
	res, err := db.Collection("bookings").DeleteMany(ctx, filter)
	if err != nil {
		log.Fatalf("rollback: %v", err)
	}
	if _, err := db.Collection("recurring_reservations").UpdateMany(ctx, bson.M{}, bson.M{"$unset": bson.M{
		"last_generated_at": "", "last_generated_count": "", "last_generation_ok": "", "generation_error": "", "generation_issues": "",
	}}); err != nil {
		log.Fatalf("rollback estado: %v", err)
	}
	log.Printf("[RECURRING] eliminadas=%d", res.DeletedCount)
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
