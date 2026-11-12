// Command remove-fintoc-bookings elimina las reservas pagadas con Fintoc, un
// proveedor que el modelo comercial retira. Estas reservas no forman parte de
// la migración (no se les calcula comisión ni se conservan en `bookings`).
//
// Una reserva es de Fintoc si payment_method="fintoc" o tiene fintoc_payment_id
// o fintoc_payment_intent_id. Antes de borrar, cada documento completo se copia
// a la colección `migration_backups` (tag fintoc-bookings-v1), lo que permite el
// ROLLBACK. Es idempotente. No toca las credenciales Fintoc de sport_centers.
//
// DRY_RUN por defecto:
//
//	MONGO_URI=... MONGO_DB=sport_booking go run ./cmd/remove-fintoc-bookings
//
// Aplicar / revertir:
//
//	APPLY=true MONGO_URI=... go run ./cmd/remove-fintoc-bookings
//	ROLLBACK=true APPLY=true MONGO_URI=... go run ./cmd/remove-fintoc-bookings
//
// Se aborta si hay reservas Fintoc confirmadas con fecha futura (hay que
// resolverlas antes con el centro); FORCE_FUTURE=true lo permite.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/hamp/booking-sport/pkg/migratelog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	migrationTag = "fintoc-bookings-v1"
	backupColl   = "migration_backups"
)

func main() {
	defer migratelog.Start("remove-fintoc-bookings")()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
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
	bookings := db.Collection("bookings")
	backups := db.Collection(backupColl)

	mode := "DRY_RUN"
	if apply {
		mode = "APPLY"
	}

	if env("ROLLBACK", "false") == "true" {
		log.Printf("[FINTOC] ROLLBACK mode=%s db=%s", mode, dbName)
		cur, err := backups.Find(ctx, bson.M{"migration": migrationTag})
		if err != nil {
			log.Fatalf("find backups: %v", err)
		}
		var all []bson.M
		if err := cur.All(ctx, &all); err != nil {
			log.Fatalf("decode backups: %v", err)
		}
		log.Printf("[FINTOC] %d reserva(s) a restaurar", len(all))
		if !apply {
			return
		}
		for _, b := range all {
			if _, err := bookings.ReplaceOne(ctx, bson.M{"_id": b["booking_id"]}, b["booking"], options.Replace().SetUpsert(true)); err != nil {
				log.Fatalf("restaurar %v: %v", b["booking_id"], err)
			}
			if _, err := backups.DeleteOne(ctx, bson.M{"_id": b["_id"]}); err != nil {
				log.Fatalf("limpiar respaldo: %v", err)
			}
		}
		log.Printf("[FINTOC] restauradas=%d", len(all))
		return
	}

	filter := bson.M{"$or": bson.A{
		bson.M{"payment_method": "fintoc"},
		bson.M{"fintoc_payment_id": bson.M{"$exists": true}},
		bson.M{"fintoc_payment_intent_id": bson.M{"$exists": true}},
	}}
	cur, err := bookings.Find(ctx, filter)
	if err != nil {
		log.Fatalf("find: %v", err)
	}
	var docs []bson.M
	if err := cur.All(ctx, &docs); err != nil {
		log.Fatalf("decode: %v", err)
	}

	byStatus := map[string]int{}
	var confirmedTotal float64
	future := 0
	for _, d := range docs {
		st, _ := d["status"].(string)
		byStatus[st]++
		if st == "confirmed" {
			if p, ok := toFloat(d["final_price"]); ok {
				confirmedTotal += p
			}
			if t, ok := d["scheduled_at"].(interface{ Time() time.Time }); ok && t.Time().After(time.Now()) {
				future++
			}
		}
	}
	log.Printf("[FINTOC] mode=%s db=%s reservas=%d por estado=%v monto confirmadas=%.0f futuras confirmadas=%d",
		mode, dbName, len(docs), byStatus, confirmedTotal, future)
	if future > 0 && env("FORCE_FUTURE", "false") != "true" {
		log.Fatalf("[FINTOC] hay %d reserva(s) Fintoc confirmadas con fecha futura: resolver con el centro o usar FORCE_FUTURE=true", future)
	}
	if !apply {
		log.Printf("[FINTOC] DRY_RUN: no se escribió nada. Repetir con APPLY=true.")
		return
	}

	for _, d := range docs {
		if _, err := backups.UpdateOne(ctx, bson.M{"migration": migrationTag, "booking_id": d["_id"]},
			bson.M{"$setOnInsert": bson.M{"migration": migrationTag, "booking_id": d["_id"], "booking": d, "created_at": time.Now().UTC()}},
			options.Update().SetUpsert(true)); err != nil {
			log.Fatalf("respaldo %v: %v", d["_id"], err)
		}
	}
	n, err := backups.CountDocuments(ctx, bson.M{"migration": migrationTag})
	if err != nil || int(n) < len(docs) {
		log.Fatalf("respaldo incompleto (%d de %d): no se borra nada", n, len(docs))
	}
	res, err := bookings.DeleteMany(ctx, filter)
	if err != nil {
		log.Fatalf("delete: %v", err)
	}
	log.Printf("[FINTOC] respaldadas=%d eliminadas=%d", n, res.DeletedCount)
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
