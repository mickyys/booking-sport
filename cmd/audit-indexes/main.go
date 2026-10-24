// Command audit-indexes lista los índices reales de las colecciones críticas
// de booking-sport y reporta anomalías conocidas. Es de sólo lectura.
//
// Uso:
//
//	MONGO_URI=mongodb://localhost:27017 go run ./cmd/audit-indexes
//	STRICT=true MONGO_URI=... go run ./cmd/audit-indexes   # exit 1 si hay anomalías
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type indexSpec struct {
	Name                    string `bson:"name"`
	Key                     bson.D `bson:"key"`
	Unique                  bool   `bson:"unique"`
	Sparse                  bool   `bson:"sparse"`
	ExpireAfterSeconds      *int32 `bson:"expireAfterSeconds"`
	PartialFilterExpression bson.M `bson:"partialFilterExpression"`
}

type anomaly struct {
	Collection string
	Index      string
	Reason     string
}

// expectedIndexes documenta los índices que el código actual garantiza. No es
// una lista exhaustiva: sólo sirve para detectar despliegues incompletos.
var expectedIndexes = map[string][]string{
	"users": {
		"idx_users_username",
	},
	"slot_holds": {
		"idx_slot_holds_unique_slot_v2",
		"idx_slot_holds_unique_local_slot",
		"idx_slot_holds_court_scheduled_at",
		"idx_slot_holds_ttl",
	},
	"bookings": {
		"idx_bookings_booking_code",
		"idx_bookings_unique_confirmed_local_slot",
		"idx_bookings_unique_pending_local_slot",
		"idx_bookings_court_scheduled_at",
	},
}

func main() {
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
		log.Fatalf("connect to MongoDB: %v", err)
	}
	defer client.Disconnect(context.Background())

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("ping MongoDB: %v", err)
	}
	db := client.Database(dbName)

	collections := []string{
		"users",
		"slot_holds",
		"bookings",
		"sport_centers",
		"spaces",
		"recurring_reservations",
	}

	var anomalies []anomaly

	for _, name := range collections {
		specs, err := listIndexes(ctx, db.Collection(name))
		if err != nil {
			log.Fatalf("list indexes for %s: %v", name, err)
		}

		fmt.Printf("\n[%s] %d índice(s)\n", name, len(specs))

		seen := make(map[string]bool, len(specs))
		for _, spec := range specs {
			seen[spec.Name] = true
			printIndex(spec)
			anomalies = append(anomalies, checkAnomalies(name, spec)...)
		}

		for _, expected := range expectedIndexes[name] {
			if !seen[expected] {
				anomalies = append(anomalies, anomaly{
					Collection: name,
					Index:      expected,
					Reason:     "índice esperado ausente (¿deploy incompleto o EnsureIndexes falló?)",
				})
			}
		}
	}

	fmt.Println("\nResumen")
	if len(anomalies) == 0 {
		fmt.Println("  sin anomalías detectadas")
	} else {
		for _, a := range anomalies {
			fmt.Printf("  ANOMALÍA %s.%s: %s\n", a.Collection, a.Index, a.Reason)
		}
		fmt.Printf("  total: %d\n", len(anomalies))
	}

	if os.Getenv("STRICT") == "true" && len(anomalies) > 0 {
		os.Exit(1)
	}
}

func listIndexes(ctx context.Context, collection *mongo.Collection) ([]indexSpec, error) {
	cursor, err := collection.Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var specs []indexSpec
	if err := cursor.All(ctx, &specs); err != nil {
		return nil, err
	}
	return specs, nil
}

func printIndex(spec indexSpec) {
	var attrs []string
	if spec.Unique {
		attrs = append(attrs, "unique")
	}
	if spec.Sparse {
		attrs = append(attrs, "sparse")
	}
	if spec.ExpireAfterSeconds != nil {
		attrs = append(attrs, fmt.Sprintf("ttl=%ds", *spec.ExpireAfterSeconds))
	}
	if len(spec.PartialFilterExpression) > 0 {
		attrs = append(attrs, fmt.Sprintf("partial=%v", spec.PartialFilterExpression))
	}

	suffix := ""
	if len(attrs) > 0 {
		suffix = " [" + strings.Join(attrs, ", ") + "]"
	}
	fmt.Printf("  - %s %v%s\n", spec.Name, keysOf(spec.Key), suffix)
}

func keysOf(key bson.D) string {
	parts := make([]string, 0, len(key))
	for _, e := range key {
		parts = append(parts, fmt.Sprintf("%s:%v", e.Key, e.Value))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func checkAnomalies(collection string, spec indexSpec) []anomaly {
	var found []anomaly

	add := func(reason string) {
		found = append(found, anomaly{
			Collection: collection,
			Index:      spec.Name,
			Reason:     reason,
		})
	}

	if collection == "users" {
		if strings.HasPrefix(spec.Name, "idx_slot_holds_") {
			add("índice de slot_holds creado sobre users (bug de ensureUserIndexes)")
		}
		if hasKey(spec.Key, "court_id") {
			add("índice con court_id sobre users; no corresponde a este modelo")
		}
	}

	if collection == "bookings" {
		switch spec.Name {
		case "idx_bookings_ttl_pending":
			add("índice TTL legacy aún presente; debe haberse retirado")
		case "idx_bookings_unique_confirmed_slot", "idx_bookings_unique_pending_slot":
			add("índice único legacy por slot; debe haberse retirado")
		}
	}

	if collection == "slot_holds" && spec.Name == "idx_slot_holds_unique_slot" {
		add("índice único legacy por slot; debe haberse retirado")
	}

	return found
}

func hasKey(key bson.D, name string) bool {
	for _, e := range key {
		if e.Key == name {
			return true
		}
	}
	return false
}
