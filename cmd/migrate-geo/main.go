package main

import (
	"context"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	dbName := os.Getenv("MONGO_DB")
	if dbName == "" {
		dbName = "sport_booking"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("Error connecting to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("Error pinging MongoDB: %v", err)
	}

	db := client.Database(dbName)
	collection := db.Collection("sport_centers")

	// 1. Crear el índice 2dsphere sobre location (idempotente).
	indexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "location", Value: "2dsphere"}},
		Options: options.Index().SetName("idx_sport_centers_location"),
	}
	if _, err := collection.Indexes().CreateOne(ctx, indexModel); err != nil {
		log.Printf("Warning creating 2dsphere index (may already exist): %v", err)
	} else {
		log.Println("[GEO] Índice 2dsphere idx_sport_centers_location creado")
	}

	// 2. Backfill: derivar location (GeoJSON [lng,lat]) desde coordinates {lat,lng}.
	cursor, err := collection.Find(ctx, bson.M{})
	if err != nil {
		log.Fatalf("Error listing sport centers: %v", err)
	}
	defer cursor.Close(ctx)

	type centerDoc struct {
		ID          primitive.ObjectID `bson:"_id"`
		Coordinates *struct {
			Lat float64 `bson:"lat"`
			Lng float64 `bson:"lng"`
		} `bson:"coordinates"`
	}

	updated := 0
	skipped := 0
	for cursor.Next(ctx) {
		var doc centerDoc
		if err := cursor.Decode(&doc); err != nil {
			log.Printf("Error decoding center: %v", err)
			continue
		}

		if doc.Coordinates == nil || (doc.Coordinates.Lat == 0 && doc.Coordinates.Lng == 0) {
			log.Printf("[GEO] Centro %s sin coordenadas válidas, se omite", doc.ID.Hex())
			skipped++
			continue
		}

		location := bson.M{
			"type":        "Point",
			"coordinates": []float64{doc.Coordinates.Lng, doc.Coordinates.Lat},
		}

		result, err := collection.UpdateOne(ctx, bson.M{"_id": doc.ID}, bson.M{
			"$set": bson.M{
				"location":   location,
				"updated_at": time.Now(),
			},
		})
		if err != nil {
			log.Printf("Error updating center %s: %v", doc.ID.Hex(), err)
			continue
		}
		updated += int(result.ModifiedCount)
		log.Printf("[GEO] Backfill location para %s: %v", doc.ID.Hex(), location)
	}

	log.Printf("[GEO] Migración completada. %d centros actualizados, %d omitidos.", updated, skipped)
}
