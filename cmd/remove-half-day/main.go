// remove-half-day elimina el campo obsoleto `price_half_day` (media jornada)
// de `spaces`, de las plantillas de onboarding en `sport_centers` y de la
// colección legacy `facilities`. Es idempotente.
//
//	MONGO_URI=... go run ./cmd/remove-half-day           # dry-run
//	MONGO_URI=... go run ./cmd/remove-half-day -apply    # escribe
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	apply := flag.Bool("apply", false, "escribe los cambios (por defecto solo cuenta)")
	flag.Parse()

	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(ctx)
	db := client.Database("sport_booking")

	targets := []struct {
		coll   string
		filter bson.M
		update bson.M
	}{
		{"spaces", bson.M{"price_half_day": bson.M{"$exists": true}}, bson.M{"$unset": bson.M{"price_half_day": ""}}},
		{"facilities", bson.M{"price_half_day": bson.M{"$exists": true}}, bson.M{"$unset": bson.M{"price_half_day": ""}}},
		{"sport_centers", bson.M{"space_templates.price_half_day": bson.M{"$exists": true}}, bson.M{"$unset": bson.M{"space_templates.$[].price_half_day": ""}}},
	}
	for _, t := range targets {
		c := db.Collection(t.coll)
		n, err := c.CountDocuments(ctx, t.filter)
		if err != nil {
			log.Fatalf("%s: %v", t.coll, err)
		}
		if !*apply {
			log.Printf("[DRY-RUN] %s: %d documentos con price_half_day", t.coll, n)
			continue
		}
		res, err := c.UpdateMany(ctx, t.filter, t.update)
		if err != nil {
			log.Fatalf("%s: %v", t.coll, err)
		}
		log.Printf("[APPLY] %s: %d modificados", t.coll, res.ModifiedCount)
	}
}
