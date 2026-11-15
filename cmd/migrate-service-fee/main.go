// Command migrate-service-fee completa payment_info.service_fee_amount en las
// reservas pagadas con cargo por servicio sumado al precio, para que se vea el
// desglose (precio + cargo) en las reservas anteriores al registro del cargo.
//
// El cargo se deriva de lo guardado: paid_amount × commission_rate / 100. Sólo
// toca reservas confirmadas con pago de Mercado Pago, creadas desde SINCE (el
// momento en que se activó el cargo sumado), con commission_rate > 0 y sin
// service_fee_amount. Es idempotente.
//
// Por defecto corre en DRY_RUN. Para aplicar:
//
//	APPLY=true SINCE=2026-11-15T05:05:00Z MONGO_URI=... go run ./cmd/migrate-service-fee
//
// Filtros opcionales: CENTER_ID=<objectid>, LIMIT=100.
package main

import (
	"context"
	"log"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/migratelog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type bookingDoc struct {
	ID          primitive.ObjectID `bson:"_id"`
	BookingCode string             `bson:"booking_code"`
	PaidAmount  float64            `bson:"paid_amount"`
	PaymentInfo struct {
		CommissionRate int `bson:"commission_rate"`
	} `bson:"payment_info"`
}

func main() {
	defer migratelog.Start("migrate-service-fee")()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	sinceRaw := os.Getenv("SINCE")
	if sinceRaw == "" {
		log.Fatal("SINCE es obligatorio (RFC3339), p. ej. 2026-11-15T05:05:00Z")
	}
	since, err := time.Parse(time.RFC3339, sinceRaw)
	if err != nil {
		log.Fatalf("SINCE inválido: %v", err)
	}
	apply := os.Getenv("APPLY") == "true"
	limit, _ := strconv.ParseInt(os.Getenv("LIMIT"), 10, 64)

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(env("MONGO_URI", "mongodb://localhost:27017")))
	if err != nil {
		log.Fatalf("connect to MongoDB: %v", err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database(env("MONGO_DB", "sport_booking"))
	col := db.Collection("bookings")

	courtIDs, err := oauthCourtIDs(ctx, db, os.Getenv("CENTER_ID"))
	if err != nil {
		log.Fatalf("load courts: %v", err)
	}
	log.Printf("[MIGRATE] canchas de centros con conexión OAuth: %d", len(courtIDs))

	filter := bson.M{
		"status":                          "confirmed",
		"mp_payment_id":                   bson.M{"$exists": true, "$ne": ""},
		"created_at":                      bson.M{"$gte": since},
		"payment_info.commission_rate":    bson.M{"$gt": 0},
		"payment_info.service_fee_amount": bson.M{"$exists": false},
		"paid_amount":                     bson.M{"$gt": 0},
	}
	if id := os.Getenv("CENTER_ID"); id != "" {
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			log.Fatalf("CENTER_ID inválido: %v", err)
		}
		filter["sport_center_id"] = oid
	}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: 1}})
	if limit > 0 {
		opts.SetLimit(limit)
	}
	cur, err := col.Find(ctx, filter, opts)
	if err != nil {
		log.Fatalf("find bookings: %v", err)
	}
	defer cur.Close(ctx)

	mode := "DRY_RUN"
	if apply {
		mode = "APPLY"
	}
	log.Printf("[MIGRATE] mode=%s since=%s", mode, since.Format(time.RFC3339))

	scanned, updated := 0, 0
	for cur.Next(ctx) {
		var b bookingDoc
		if err := cur.Decode(&b); err != nil {
			log.Fatalf("decode: %v", err)
		}
		scanned++
		fee := math.Round(b.PaidAmount * float64(b.PaymentInfo.CommissionRate) / 100)
		if fee <= 0 {
			continue
		}
		log.Printf("[MIGRATE] %s paid=%.0f rate=%d%% -> service_fee=%.0f", b.BookingCode, b.PaidAmount, b.PaymentInfo.CommissionRate, fee)
		if !apply {
			continue
		}
		res, err := col.UpdateOne(ctx,
			bson.M{"_id": b.ID, "payment_info.service_fee_amount": bson.M{"$exists": false}},
			bson.M{"$set": bson.M{"payment_info.service_fee_amount": fee}})
		if err != nil {
			log.Fatalf("update %s: %v", b.BookingCode, err)
		}
		updated += int(res.ModifiedCount)
	}
	if err := cur.Err(); err != nil {
		log.Fatalf("cursor: %v", err)
	}
	log.Printf("[MIGRATE] done mode=%s scanned=%d updated=%d", mode, scanned, updated)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// oauthCourtIDs devuelve las canchas de centros conectados por OAuth: sólo
// ellos reciben el cargo por servicio sumado al precio.
func oauthCourtIDs(ctx context.Context, db *mongo.Database, centerID string) ([]primitive.ObjectID, error) {
	cur, err := db.Collection("sport_centers").Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	var centers []domain.SportCenter
	if err := cur.All(ctx, &centers); err != nil {
		return nil, err
	}
	allowed := map[primitive.ObjectID]bool{}
	for i := range centers {
		c := &centers[i]
		if centerID != "" && c.ID.Hex() != centerID {
			continue
		}
		if c.MercadoPago.SupportsMarketplaceFee() {
			allowed[c.ID] = true
		}
	}
	ccur, err := db.Collection("courts").Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"sport_center_id": 1}))
	if err != nil {
		return nil, err
	}
	var courts []struct {
		ID     primitive.ObjectID `bson:"_id"`
		Center primitive.ObjectID `bson:"sport_center_id"`
	}
	if err := ccur.All(ctx, &courts); err != nil {
		return nil, err
	}
	ids := []primitive.ObjectID{}
	for _, c := range courts {
		if allowed[c.Center] {
			ids = append(ids, c.ID)
		}
	}
	return ids, nil
}
