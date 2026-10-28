// Command migrate-resolve-duplicates cancela reservas y recurrentes duplicadas
// detectadas antes de la migración (ver docs/plan-migracion-modelo-comercial.md,
// Fase 1). No borra nada: pasa a `cancelled` los documentos indicados, con
// cancelled_by="migration:phase1" y el motivo, y guarda el documento original
// en `migration_backups` (tag duplicates-v1) para el ROLLBACK.
//
//   - BOOKING_CODES=a,b,c   reservas (booking_code) confirmadas a cancelar
//   - RECURRING_IDS=id1,id2 recurrentes activas a cancelar
//
// DRY_RUN por defecto:
//
//	BOOKING_CODES=633c7e11,3a46b3e1,b5427ce4 RECURRING_IDS=69e6ca4e5526854c22ccd068 \
//	  MONGO_URI=... MONGO_DB=sport_booking go run ./cmd/migrate-resolve-duplicates
//
// Aplicar / revertir: APPLY=true [ROLLBACK=true] con las mismas variables
// (el rollback restaura todo lo respaldado con este tag).
package main

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/hamp/booking-sport/pkg/migratelog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	migrationTag = "duplicates-v1"
	backupColl   = "migration_backups"
)

func main() {
	defer migratelog.Start("migrate-resolve-duplicates")()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	apply := env("APPLY", "false") == "true"
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(env("MONGO_URI", "mongodb://localhost:27017")))
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database(env("MONGO_DB", "sport_booking"))
	backups := db.Collection(backupColl)

	mode := "DRY_RUN"
	if apply {
		mode = "APPLY"
	}

	if env("ROLLBACK", "false") == "true" {
		log.Printf("[DUPLICATES] ROLLBACK mode=%s", mode)
		cur, err := backups.Find(ctx, bson.M{"migration": migrationTag})
		if err != nil {
			log.Fatalf("find backups: %v", err)
		}
		var all []bson.M
		if err := cur.All(ctx, &all); err != nil {
			log.Fatalf("decode backups: %v", err)
		}
		log.Printf("[DUPLICATES] %d documento(s) a restaurar", len(all))
		if !apply {
			return
		}
		for _, b := range all {
			coll := db.Collection(b["collection"].(string))
			if _, err := coll.ReplaceOne(ctx, bson.M{"_id": b["doc_id"]}, b["doc"], options.Replace().SetUpsert(true)); err != nil {
				log.Fatalf("restaurar %v: %v", b["doc_id"], err)
			}
			if _, err := backups.DeleteOne(ctx, bson.M{"_id": b["_id"]}); err != nil {
				log.Fatalf("limpiar respaldo: %v", err)
			}
			log.Printf("[DUPLICATES] restaurado %s %v", b["collection"], b["doc_id"])
		}
		return
	}

	codes := split(os.Getenv("BOOKING_CODES"))
	recs := split(os.Getenv("RECURRING_IDS"))
	if len(codes) == 0 && len(recs) == 0 {
		log.Fatalf("indicar BOOKING_CODES y/o RECURRING_IDS")
	}
	log.Printf("[DUPLICATES] mode=%s reservas=%v recurrentes=%v", mode, codes, recs)

	now := time.Now().UTC()
	changed := 0
	for _, code := range codes {
		var doc bson.M
		err := db.Collection("bookings").FindOne(ctx, bson.M{"booking_code": code}).Decode(&doc)
		if err != nil {
			log.Fatalf("reserva %s: %v", code, err)
		}
		if doc["status"] != "confirmed" {
			log.Printf("[DUPLICATES] reserva %s ya está %v: se omite", code, doc["status"])
			continue
		}
		log.Printf("[DUPLICATES] reserva %s %v %v h%v cliente=%v -> cancelled", code, doc["court_name"], doc["local_date"], doc["hour"], doc["customer_name"])
		if apply {
			backup(ctx, backups, "bookings", doc)
			if _, err := db.Collection("bookings").UpdateOne(ctx, bson.M{"_id": doc["_id"]}, bson.M{"$set": bson.M{
				"status": "cancelled", "cancelled_by": "migration:phase1",
				"cancellation_reason": "Reserva duplicada del mismo horario detectada en la migración de esquema (fase 1)",
				"updated_at":          now,
			}}); err != nil {
				log.Fatalf("cancelar %s: %v", code, err)
			}
		}
		changed++
	}
	for _, id := range recs {
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			log.Fatalf("recurrente %s: id inválido", id)
		}
		var doc bson.M
		if err := db.Collection("recurring_reservations").FindOne(ctx, bson.M{"_id": oid}).Decode(&doc); err != nil {
			log.Fatalf("recurrente %s: %v", id, err)
		}
		if doc["status"] != "active" {
			log.Printf("[DUPLICATES] recurrente %s ya está %v: se omite", id, doc["status"])
			continue
		}
		log.Printf("[DUPLICATES] recurrente %s %v dow=%v h%v -> cancelled", id, doc["customer_name"], doc["day_of_week"], doc["hour"])
		if apply {
			backup(ctx, backups, "recurring_reservations", doc)
			if _, err := db.Collection("recurring_reservations").UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{
				"status": "cancelled", "cancelled_by": "migration:phase1",
				"cancel_reason": "Recurrente duplicada (mismo cliente, cancha, día y hora)",
				"updated_at":    now,
			}}); err != nil {
				log.Fatalf("cancelar recurrente %s: %v", id, err)
			}
		}
		changed++
	}
	log.Printf("[DUPLICATES] resumen cancelados=%d", changed)
	if !apply {
		log.Printf("[DUPLICATES] DRY_RUN: no se escribió nada. Repetir con APPLY=true.")
	}
}

func backup(ctx context.Context, backups *mongo.Collection, coll string, doc bson.M) {
	if _, err := backups.UpdateOne(ctx, bson.M{"migration": migrationTag, "collection": coll, "doc_id": doc["_id"]},
		bson.M{"$setOnInsert": bson.M{"migration": migrationTag, "collection": coll, "doc_id": doc["_id"], "doc": doc, "created_at": time.Now().UTC()}},
		options.Update().SetUpsert(true)); err != nil {
		log.Fatalf("respaldo %v: %v", doc["_id"], err)
	}
}

func split(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
