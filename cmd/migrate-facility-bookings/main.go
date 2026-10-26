// migrate-facility-bookings copia las reservas de sedes y piscinas de la
// colección legacy `facility_bookings` a `bookings` (una reserva por día, de
// día completo), conservando el _id para que los enlaces existentes sigan
// funcionando. Es idempotente: los _id ya migrados se omiten.
//
// Uso:
//
//	MONGO_URI=... go run ./cmd/migrate-facility-bookings            # dry-run
//	MONGO_URI=... go run ./cmd/migrate-facility-bookings -apply     # escribe
//
// La colección legacy no se modifica ni se borra: sirve de respaldo hasta
// validar la reconciliación que imprime al final.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/migratelog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	defer migratelog.Start("migrate-facility-bookings")()
	apply := flag.Bool("apply", false, "escribe en bookings (por defecto solo simula)")
	flag.Parse()

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(ctx)

	db := client.Database("sport_booking")
	legacy := db.Collection("facility_bookings")
	bookings := db.Collection("bookings")

	cursor, err := legacy.Find(ctx, bson.M{})
	if err != nil {
		log.Fatal(err)
	}
	defer cursor.Close(ctx)

	var total, migrated, skipped, conflicts, failed int
	var legacyPaid, migratedPaid float64

	for cursor.Next(ctx) {
		var fb domain.FacilityBooking
		if err := cursor.Decode(&fb); err != nil {
			failed++
			log.Printf("decode: %v", err)
			continue
		}
		total++
		legacyPaid += fb.PaidAmount

		exists, err := bookings.CountDocuments(ctx, bson.M{"_id": fb.ID})
		if err != nil {
			failed++
			log.Printf("%s: %v", fb.ID.Hex(), err)
			continue
		}
		if exists > 0 {
			skipped++
			continue
		}

		b, err := app.FacilityBookingToBooking(&fb)
		if err != nil {
			failed++
			log.Printf("%s: %v", fb.ID.Hex(), err)
			continue
		}
		if err := b.PrepareForWrite(nil); err != nil {
			failed++
			log.Printf("%s: %v", fb.ID.Hex(), err)
			continue
		}
		b.Version = 1
		if fb.Status == domain.BookingStatusCancelled || fb.Status == domain.BookingStatusExpired {
			b.CancelledBy = fb.CancelledBy
		}

		if !*apply {
			migrated++
			migratedPaid += fb.PaidAmount
			continue
		}
		if _, err := bookings.InsertOne(ctx, b); err != nil {
			if mongo.IsDuplicateKeyError(err) {
				conflicts++
				log.Printf("conflicto (día ya ocupado en bookings): %s %s %s", fb.ID.Hex(), fb.SpaceName, fb.LocalDate)
				continue
			}
			failed++
			log.Printf("%s: %v", fb.ID.Hex(), err)
			continue
		}
		migrated++
		migratedPaid += fb.PaidAmount
	}

	mode := "DRY-RUN"
	if *apply {
		mode = "APPLY"
	}
	log.Printf("[%s] legacy=%d migrados=%d omitidos(ya existían)=%d conflictos=%d errores=%d", mode, total, migrated, skipped, conflicts, failed)
	log.Printf("[%s] monto pagado legacy=%.0f migrado=%.0f", mode, legacyPaid, migratedPaid)
	if failed > 0 || conflicts > 0 {
		os.Exit(1)
	}
}
