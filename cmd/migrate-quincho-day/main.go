// Command migrate-quincho-day convierte los quinchos que en producción se
// guardaron como canchas (colección `courts`, nombre "Quincho N", una franja
// por hora) en espacios de categoría `quincho` que se reservan por día
// completo, igual que sedes y piscinas.
//
// Por cada espacio seleccionado:
//   - spaces: category=quincho, type=quincho, price_all_day = precio de la
//     franja legacy. Se conserva el _id, por lo que las reservas siguen
//     apuntando al mismo espacio.
//   - schedule_rules: se reemplazan por una regla de día completo (00:00-24:00).
//   - bookings: cada reserva pasa a día completo (hour=0, minutes=0,
//     duration_minutes=1440, scheduled_at = primer instante válido del día
//     local y ends_at = primer instante válido del día siguiente). El precio,
//     estado, código y datos del cliente no cambian.
//
// Antes de escribir, el estado original de cada espacio, sus reglas y sus
// reservas se guarda en la colección `migration_backups` (tag quincho-day-v1),
// que permite el ROLLBACK. Es idempotente: los espacios que ya son `quincho`
// se omiten.
//
// Selección: por defecto los espacios `cancha` cuyo nombre empieza con
// "Quincho". Se puede acotar con CENTER_ID o fijar con SPACE_IDS=id1,id2 y
// NAME_PATTERN=<regex>.
//
// DRY_RUN por defecto:
//
//	MONGO_URI=... MONGO_DB=sport_booking go run ./cmd/migrate-quincho-day
//
// Aplicar / revertir:
//
//	APPLY=true MONGO_URI=... MONGO_DB=... go run ./cmd/migrate-quincho-day
//	ROLLBACK=true APPLY=true MONGO_URI=... MONGO_DB=... go run ./cmd/migrate-quincho-day
//
// Importante: migrate-spaces vuelve a poner category=cancha en los espacios que
// vienen de `courts`; ejecutarlo siempre ANTES de este comando.
package main

import (
	"context"
	"fmt"
	"log"
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

const (
	migrationTag  = "quincho-day-v1"
	dayMinutes    = 24 * 60
	backupColName = "migration_backups"
)

func main() {
	defer migratelog.Start("migrate-quincho-day")()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	mongoURI := env("MONGO_URI", "mongodb://localhost:27017")
	dbName := env("MONGO_DB", "sport_booking")
	apply := env("APPLY", "false") == "true"
	rollback := env("ROLLBACK", "false") == "true"

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database(dbName)

	mode := "DRY_RUN"
	if apply {
		mode = "APPLY"
	}

	if rollback {
		log.Printf("[QUINCHO] ROLLBACK mode=%s db=%s", mode, dbName)
		runRollback(ctx, db, apply)
		return
	}

	filter := bson.M{"category": "cancha", "name": bson.M{"$regex": env("NAME_PATTERN", "^Quincho"), "$options": "i"}}
	if v := os.Getenv("CENTER_ID"); v != "" {
		id, err := primitive.ObjectIDFromHex(v)
		if err != nil {
			log.Fatalf("CENTER_ID inválido: %v", err)
		}
		filter["sport_center_id"] = id
	}
	if v := os.Getenv("SPACE_IDS"); v != "" {
		var ids bson.A
		for _, s := range strings.Split(v, ",") {
			id, err := primitive.ObjectIDFromHex(strings.TrimSpace(s))
			if err != nil {
				log.Fatalf("SPACE_IDS inválido: %v", err)
			}
			ids = append(ids, id)
		}
		filter = bson.M{"category": "cancha", "_id": bson.M{"$in": ids}}
	}

	spaces := db.Collection("spaces")
	cur, err := spaces.Find(ctx, filter)
	if err != nil {
		log.Fatalf("find spaces: %v", err)
	}
	var selected []bson.M
	if err := cur.All(ctx, &selected); err != nil {
		log.Fatalf("decode spaces: %v", err)
	}

	log.Printf("[QUINCHO] mode=%s db=%s espacios=%d", mode, dbName, len(selected))
	converted, review := 0, 0
	for _, sp := range selected {
		ok, err := convert(ctx, db, sp, apply)
		if err != nil {
			log.Fatalf("[QUINCHO] %v: %v", sp["_id"], err)
		}
		if ok {
			converted++
		} else {
			review++
		}
	}
	log.Printf("[QUINCHO] resumen convertidos=%d omitidos(revisar)=%d", converted, review)
	if apply {
		recomputeCourtsCount(ctx, db)
	}
	if !apply {
		log.Printf("[QUINCHO] DRY_RUN: no se escribió nada. Repetir con APPLY=true.")
	}
}

// allDayPrice deduce el precio por día desde las franjas legacy. Si las
// franjas tienen precios distintos no se asume ninguno.
func allDayPrice(sp bson.M) (float64, bool) {
	slots, _ := sp["schedule"].(bson.A)
	price := -1.0
	for _, s := range slots {
		m, _ := s.(bson.M)
		p, ok := toFloat(m["price"])
		if !ok {
			continue
		}
		if price >= 0 && p != price {
			return 0, false
		}
		price = p
	}
	if price <= 0 {
		return 0, false
	}
	return price, true
}

func convert(ctx context.Context, db *mongo.Database, sp bson.M, apply bool) (bool, error) {
	id := sp["_id"].(primitive.ObjectID)
	name, _ := sp["name"].(string)
	centerID, _ := sp["sport_center_id"].(primitive.ObjectID)

	price, ok := allDayPrice(sp)
	if !ok {
		log.Printf("[QUINCHO] %s %-12s OMITIDO: sin precio único en las franjas legacy (revisar y fijar manualmente)", id.Hex(), name)
		return false, nil
	}

	bookings := db.Collection("bookings")
	cur, err := bookings.Find(ctx, bson.M{"court_id": id})
	if err != nil {
		return false, err
	}
	var docs []bson.M
	if err := cur.All(ctx, &docs); err != nil {
		return false, err
	}
	rules, err := db.Collection("schedule_rules").Find(ctx, bson.M{"space_id": id})
	if err != nil {
		return false, err
	}
	var ruleDocs []bson.M
	if err := rules.All(ctx, &ruleDocs); err != nil {
		return false, err
	}

	type change struct {
		id  primitive.ObjectID
		set bson.M
	}
	var changes []change
	seen := map[string]string{}
	for _, b := range docs {
		localDate, _ := b["local_date"].(string)
		if localDate == "" {
			return false, fmt.Errorf("reserva %v sin local_date: ejecutar migrate-booking-schedule-fields antes", b["_id"])
		}
		day, err := domain.ParseSantiagoDate(localDate)
		if err != nil {
			return false, fmt.Errorf("reserva %v: %w", b["_id"], err)
		}
		start, next := domain.SantiagoDayBounds(day)
		if status, _ := b["status"].(string); status == "confirmed" || status == "pending" {
			if other, dup := seen[localDate+"|"+status]; dup {
				return false, fmt.Errorf("dos reservas %s el %s (%s y %v): resolver duplicados antes", status, localDate, other, b["_id"])
			}
			seen[localDate+"|"+status] = fmt.Sprint(b["_id"])
		}
		changes = append(changes, change{id: b["_id"].(primitive.ObjectID), set: bson.M{
			"date":                    domain.SantiagoDateStart(day),
			"hour":                    0,
			"minutes":                 0,
			"duration_minutes":        dayMinutes,
			"scheduled_at":            start.UTC(),
			"ends_at":                 next.UTC(),
			"timezone":                domain.SantiagoTimezone,
			"schedule_schema_version": domain.ScheduleSchemaVersionUTC,
			"space_category":          "quincho",
		}})
	}

	log.Printf("[QUINCHO] %s %-12s -> quincho día completo, price_all_day=%.0f, reservas=%d, reglas=%d", id.Hex(), name, price, len(docs), len(ruleDocs))
	if !apply {
		return true, nil
	}

	backup := bson.M{
		"migration": migrationTag, "space_id": id, "created_at": time.Now().UTC(),
		"space": sp, "rules": ruleDocs, "bookings": slimBookings(docs),
	}
	backups := db.Collection(backupColName)
	if _, err := backups.UpdateOne(ctx, bson.M{"migration": migrationTag, "space_id": id},
		bson.M{"$setOnInsert": backup}, options.Update().SetUpsert(true)); err != nil {
		return false, fmt.Errorf("respaldo: %w", err)
	}

	for _, c := range changes {
		if _, err := bookings.UpdateOne(ctx, bson.M{"_id": c.id}, bson.M{"$set": c.set}); err != nil {
			return false, fmt.Errorf("reserva %v: %w", c.id, err)
		}
	}

	now := time.Now().UTC()
	if _, err := db.Collection("schedule_rules").DeleteMany(ctx, bson.M{"space_id": id}); err != nil {
		return false, err
	}
	if _, err := db.Collection("schedule_rules").InsertOne(ctx, bson.M{
		"space_id": id, "sport_center_id": centerID, "days_of_week": bson.A{0, 1, 2, 3, 4, 5, 6},
		"slots": bson.A{bson.M{
			"start_time": "00:00", "end_time": "24:00", "price": price,
			"payment_required": false, "payment_optional": false,
		}},
		"active": true, "created_at": now, "updated_at": now,
	}); err != nil {
		return false, err
	}
	if _, err := db.Collection("spaces").UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set":   bson.M{"category": "quincho", "type": "quincho", "price_all_day": price, "updated_at": now},
		"$unset": bson.M{"schedule": ""},
	}); err != nil {
		return false, err
	}
	return true, nil
}

// slimBookings guarda solo los campos temporales que cambia la migración.
func slimBookings(docs []bson.M) []bson.M {
	keys := []string{"date", "hour", "minutes", "duration_minutes", "scheduled_at", "ends_at", "timezone", "schedule_schema_version", "space_category"}
	out := make([]bson.M, 0, len(docs))
	for _, b := range docs {
		m := bson.M{"_id": b["_id"]}
		for _, k := range keys {
			if v, ok := b[k]; ok {
				m[k] = v
			}
		}
		out = append(out, m)
	}
	return out
}

func runRollback(ctx context.Context, db *mongo.Database, apply bool) {
	backups := db.Collection(backupColName)
	cur, err := backups.Find(ctx, bson.M{"migration": migrationTag})
	if err != nil {
		log.Fatalf("find backups: %v", err)
	}
	var all []bson.M
	if err := cur.All(ctx, &all); err != nil {
		log.Fatalf("decode backups: %v", err)
	}
	log.Printf("[QUINCHO] %d espacio(s) a revertir", len(all))
	if !apply {
		return
	}
	for _, b := range all {
		id := b["space_id"].(primitive.ObjectID)
		if bs, ok := b["bookings"].(bson.A); ok {
			for _, x := range bs {
				m := x.(bson.M)
				bid := m["_id"]
				delete(m, "_id")
				unset := bson.M{}
				if _, ok := m["space_category"]; !ok {
					unset["space_category"] = ""
				}
				upd := bson.M{"$set": m}
				if len(unset) > 0 {
					upd["$unset"] = unset
				}
				if _, err := db.Collection("bookings").UpdateOne(ctx, bson.M{"_id": bid}, upd); err != nil {
					log.Fatalf("rollback reserva %v: %v", bid, err)
				}
			}
		}
		if _, err := db.Collection("schedule_rules").DeleteMany(ctx, bson.M{"space_id": id}); err != nil {
			log.Fatalf("rollback reglas: %v", err)
		}
		if rs, ok := b["rules"].(bson.A); ok && len(rs) > 0 {
			docs := make([]interface{}, 0, len(rs))
			docs = append(docs, rs...)
			if _, err := db.Collection("schedule_rules").InsertMany(ctx, docs); err != nil {
				log.Fatalf("rollback reglas: %v", err)
			}
		}
		if _, err := db.Collection("spaces").ReplaceOne(ctx, bson.M{"_id": id}, b["space"]); err != nil {
			log.Fatalf("rollback espacio: %v", err)
		}
		if _, err := backups.DeleteOne(ctx, bson.M{"_id": b["_id"]}); err != nil {
			log.Fatalf("rollback backup: %v", err)
		}
		log.Printf("[QUINCHO] revertido %s", id.Hex())
	}
	recomputeCourtsCount(ctx, db)
}

// recomputeCourtsCount deja `courts_count` en la cantidad de espacios de
// categoría cancha de cada centro (los quinchos ya no cuentan como canchas).
func recomputeCourtsCount(ctx context.Context, db *mongo.Database) {
	cur, err := db.Collection("sport_centers").Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		log.Fatalf("centers: %v", err)
	}
	var centers []bson.M
	if err := cur.All(ctx, &centers); err != nil {
		log.Fatalf("centers: %v", err)
	}
	for _, c := range centers {
		id := c["_id"].(primitive.ObjectID)
		n, err := db.Collection("spaces").CountDocuments(ctx, bson.M{"sport_center_id": id, "category": "cancha"})
		if err != nil {
			log.Fatalf("count: %v", err)
		}
		if _, err := db.Collection("sport_centers").UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"courts_count": n}}); err != nil {
			log.Fatalf("courts_count: %v", err)
		}
		log.Printf("[QUINCHO] courts_count %s = %d", id.Hex(), n)
	}
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
