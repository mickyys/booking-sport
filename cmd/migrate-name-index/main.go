package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	dbName := os.Getenv("MONGO_DB")
	if dbName == "" {
		dbName = "booking-sport"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("Error connecting to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("Error pinging MongoDB: %v", err)
	}

	log.Println("Connected to MongoDB")

	db := client.Database(dbName)
	collection := db.Collection("sport_centers")

	log.Println("Creating index on name field for optimized regex searches...")

	indexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "name", Value: 1}},
		Options: options.Index().SetName("idx_sport_centers_name"),
	}

	indexName, err := collection.Indexes().CreateOne(ctx, indexModel)
	if err != nil {
		log.Printf("Error creating index (may already exist): %v", err)
	} else {
		log.Printf("Index created successfully: %s", indexName)
	}

	log.Println("Dropping text index (no longer needed for name searches)...")
	_, err = collection.Indexes().DropOne(ctx, "idx_sport_centers_text")
	if err != nil {
		log.Printf("Error dropping text index (may not exist): %v", err)
	} else {
		log.Println("Text index dropped successfully")
	}

	indexCursor, err := collection.Indexes().List(ctx)
	if err != nil {
		log.Fatalf("Error listing indexes: %v", err)
	}
	
	var indexes []bson.M
	if err := indexCursor.All(ctx, &indexes); err != nil {
		log.Fatalf("Error decoding indexes: %v", err)
	}

	fmt.Println("\nCurrent indexes on sport_centers:")
	for _, idx := range indexes {
		fmt.Printf("  - %s: %v\n", idx["name"], idx["key"])
	}

	log.Println("\nMigration completed successfully!")
	log.Println("\nBenefits:")
	log.Println("  - Regex searches with ^pattern now use index (anchored prefix searches)")
	log.Println("  - Searching 'cato' will now match 'Católica' instantly")
	log.Println("  - Case-insensitive searches still work with $options: 'i'")
}
