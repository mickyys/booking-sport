package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/hamp/booking-sport/pkg/migratelog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type tenantSeed struct {
	Slug      string            `json:"slug"`
	Subdomain string            `json:"subdomain"`
	Domains   []string          `json:"domains"`
	Branding  map[string]string `json:"branding"`
}

func main() {
	defer migratelog.Start("migrate-tenants")()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

	log.Println("Creating indexes for subdomain and domains...")
	for _, indexModel := range []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "subdomain", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true).SetName("idx_sport_centers_subdomain"),
		},
		{
			Keys:    bson.D{{Key: "domains", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true).SetName("idx_sport_centers_domains"),
		},
	} {
		if _, err := collection.Indexes().CreateOne(ctx, indexModel); err != nil {
			log.Printf("Warning creating index (may already exist): %v", err)
		}
	}

	seeds := loadSeeds()

	updated := 0
	for _, seed := range seeds {
		if seed.Subdomain == "" {
			continue
		}
		center := collection.FindOne(ctx, bson.M{"slug": seed.Slug})
		if center.Err() != nil {
			log.Printf("Center with slug %q not found, skipping", seed.Slug)
			continue
		}

		domains := seed.Domains
		if len(domains) == 0 {
			domains = buildDefaultDomains(seed.Subdomain)
		}
		branding := bson.M{}
		for k, v := range seed.Branding {
			if v != "" {
				branding[k] = v
			}
		}

		update := bson.M{
			"subdomain":  seed.Subdomain,
			"domains":    domains,
			"updated_at": time.Now(),
		}
		if len(branding) > 0 {
			update["branding"] = branding
		}

		result, err := collection.UpdateOne(ctx, bson.M{"slug": seed.Slug}, bson.M{"$set": update})
		if err != nil {
			log.Printf("Error updating center %q: %v", seed.Slug, err)
			continue
		}
		updated += int(result.ModifiedCount)
		log.Printf("Seeded tenant: slug=%s subdomain=%s domains=%v", seed.Slug, seed.Subdomain, domains)
	}

	log.Printf("Migration completed. %d centers updated.", updated)
	log.Println("For more tenants, provide a JSON file with TENANTS_FILE (see cmd/migrate-tenants).")
}

func platformDomain() string {
	if d := os.Getenv("PLATFORM_DOMAIN"); d != "" {
		return d
	}
	return "reservaloya.cl"
}

func buildDefaultDomains(subdomain string) []string {
	host := subdomain + "." + platformDomain()
	return []string{host, "www." + host}
}

func loadSeeds() []tenantSeed {
	defaultSeeds := []tenantSeed{
		{
			Slug:      "catolica",
			Subdomain: "catolica",
			Branding:  map[string]string{"name": ""},
		},
		{
			Slug:      "orellana",
			Subdomain: "orellana",
			Branding:  map[string]string{"name": ""},
		},
	}

	file := os.Getenv("TENANTS_FILE")
	if file == "" {
		return defaultSeeds
	}

	data, err := os.ReadFile(file)
	if err != nil {
		log.Printf("Warning: could not read TENANTS_FILE %q: %v. Using defaults.", file, err)
		return defaultSeeds
	}

	var seeds []tenantSeed
	if err := json.Unmarshal(data, &seeds); err != nil {
		log.Printf("Warning: invalid JSON in TENANTS_FILE %q: %v. Using defaults.", file, err)
		return defaultSeeds
	}
	return seeds
}
