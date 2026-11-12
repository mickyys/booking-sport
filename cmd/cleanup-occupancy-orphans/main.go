// Command cleanup-occupancy-orphans elimina de Redis las ocupaciones que ya no
// tienen reserva en MongoDB. Las claves de ocupación no tienen TTL, así que
// esas entradas bloquean horarios para siempre: el reconciliador sólo recorre
// reservas enlazadas y nunca las ve.
//
// Uso (dry-run por defecto):
//
//	MONGO_URI=... REDIS_URL=rediss://... go run ./cmd/cleanup-occupancy-orphans
//	APPLY=true MONGO_URI=... REDIS_URL=... go run ./cmd/cleanup-occupancy-orphans
//
// Sólo toca namespaces cuyo centro y espacio existen en la base apuntada por
// MONGO_URI. Las claves de otro entorno que comparta la misma instancia de
// Redis se omiten y se reportan, para no borrar ocupaciones ajenas.
//
// Reglas:
//   - Ocupación cuya reserva no existe en MongoDB: se elimina.
//   - Hold vencido (state=held y expires<=ahora) sin reserva: se elimina.
//   - Hold vigente sin reserva: se deja (hay un checkout en curso).
//   - Reserva existente con otro occupancy_id: se reporta, no se borra.
//   - Reserva existente y enlazada: no se toca; eso es trabajo de
//     reconcile-occupancy.
//
// Env: MONGO_URI, MONGO_DB, REDIS_URL, REDIS_KEY_PREFIX, APPLY.
package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hamp/booking-sport/internal/app"
	goredis "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const defaultTimezone = "America/Santiago"

// occupancy es una entrada del hash <prefix>:occ:{centro:espacio}:m.
type occupancy struct {
	key      string
	centerID primitive.ObjectID
	spaceID  primitive.ObjectID
	id       string
	start    time.Time
	end      time.Time
	state    string
	expires  time.Time // cero cuando no aplica
	booking  string    // vacío en holds que todavía no tienen reserva
}

type candidate struct {
	entry  occupancy
	reason string
}

type bookingRef struct {
	occupancyID string
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	prefix := env("REDIS_KEY_PREFIX", "reservaloya")
	apply := env("APPLY", "false") == "true"

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(env("MONGO_URI", "mongodb://localhost:27017")))
	if err != nil {
		log.Fatalf("connect MongoDB: %v", err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database(env("MONGO_DB", "sport_booking"))

	rdb, err := redisClient(ctx, env("REDIS_URL", ""))
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	defer rdb.Close()

	centers := loadCenters(ctx, db)
	spaces := loadSpaces(ctx, db)

	keys, err := scanKeys(ctx, rdb, prefix)
	if err != nil {
		log.Fatalf("scan redis: %v", err)
	}

	mode := "DRY_RUN"
	if apply {
		mode = "APPLY"
	}
	log.Printf("[ORPHANS] mode=%s prefix=%s keys=%d", mode, prefix, len(keys))

	var entries []occupancy
	skippedNamespaces := 0
	var corrupt int
	for _, key := range keys {
		centerID, spaceID, ok := parseNamespace(key, prefix)
		if !ok || !knownNamespace(centerID, spaceID, centers, spaces) {
			skippedNamespaces++
			log.Printf("[ORPHANS] namespace ajeno o inválido (se omite): %s", key)
			continue
		}
		raw, err := rdb.HGetAll(ctx, key).Result()
		if err != nil {
			log.Fatalf("hgetall %s: %v", key, err)
		}
		for id, value := range raw {
			entry, ok := parseEntry(key, centerID, spaceID, id, value)
			if !ok {
				corrupt++
				continue
			}
			entries = append(entries, entry)
		}
	}

	refs := loadBookings(ctx, db, entries)

	now := time.Now()
	var candidates []candidate
	linked, orphan, expiredHold, mismatch := 0, 0, 0, 0
	for _, e := range entries {
		ref, exists := refs[e.booking]
		switch {
		case e.booking == "":
			if e.state == string(app.OccupancyHeld) && !e.expires.IsZero() && !e.expires.After(now) {
				expiredHold++
				candidates = append(candidates, candidate{e, "hold_vencido"})
			}
			// Hold vigente: checkout en curso, no se toca.
		case !exists:
			orphan++
			candidates = append(candidates, candidate{e, "reserva_inexistente"})
		case ref.occupancyID != e.id:
			mismatch++
			log.Printf("[ORPHANS] MISMATCH booking=%s occ_en_redis=%s occ_en_mongo=%q", e.booking, e.id, ref.occupancyID)
		default:
			linked++
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].entry.start.Before(candidates[j].entry.start) })

	log.Printf("[ORPHANS] entradas=%d enlazadas=%d sin_reserva=%d hold_vencido=%d mismatch=%d corruptas=%d namespaces_omitidos=%d",
		len(entries), linked, orphan, expiredHold, mismatch, corrupt, skippedNamespaces)
	for _, c := range candidates {
		loc := timezone(centers[c.entry.centerID])
		fmt.Printf("  %-18s %s-%s %s space=%s booking=%s occ=%s\n",
			c.reason,
			c.entry.start.In(loc).Format("2006-01-02 15:04"),
			c.entry.end.In(loc).Format("15:04"),
			loc.String(),
			c.entry.spaceID.Hex(),
			orDash(c.entry.booking),
			c.entry.id)
	}

	if !apply {
		log.Printf("[ORPHANS] DRY_RUN: nada borrado (%d candidatas). Re-ejecutar con APPLY=true.", len(candidates))
		return
	}
	if len(candidates) == 0 {
		log.Printf("[ORPHANS] nada que borrar.")
		return
	}

	pipe := rdb.Pipeline()
	for _, c := range candidates {
		pipe.HDel(ctx, c.entry.key, c.entry.id)
		pipe.ZRem(ctx, strings.TrimSuffix(c.entry.key, ":m")+":z", c.entry.id)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		log.Fatalf("delete: %v", err)
	}
	log.Printf("[ORPHANS] borradas=%d", len(candidates))
}

func redisClient(ctx context.Context, rawURL string) (*goredis.Client, error) {
	if rawURL == "" {
		return nil, fmt.Errorf("REDIS_URL is required")
	}
	// Los errores de url.Parse/ParseURL incluyen la URL completa (con el token):
	// no se propagan para que nunca terminen en logs.
	if _, err := url.Parse(rawURL); err != nil {
		return nil, fmt.Errorf("invalid REDIS_URL (details omitted to avoid leaking credentials)")
	}
	opts, err := goredis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_URL (details omitted to avoid leaking credentials)")
	}
	opts.Protocol = 2
	opts.PoolSize = 5
	opts.DialTimeout = 5 * time.Second
	opts.ReadTimeout = 5 * time.Second
	opts.WriteTimeout = 5 * time.Second
	client := goredis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis unavailable: %w", err)
	}
	return client, nil
}

func scanKeys(ctx context.Context, rdb *goredis.Client, prefix string) ([]string, error) {
	var keys []string
	iter := rdb.Scan(ctx, 0, prefix+":occ:*:m", 1000).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		if strings.HasSuffix(key, ":m") {
			keys = append(keys, key)
		}
	}
	return keys, iter.Err()
}

func parseNamespace(key, prefix string) (centerID, spaceID primitive.ObjectID, ok bool) {
	tag := strings.TrimPrefix(key, prefix+":occ:{")
	if tag == key {
		return centerID, spaceID, false
	}
	tag = strings.TrimSuffix(tag, "}:m")
	parts := strings.Split(tag, ":")
	if len(parts) != 2 {
		return centerID, spaceID, false
	}
	centerID, err := primitive.ObjectIDFromHex(parts[0])
	if err != nil {
		return centerID, spaceID, false
	}
	spaceID, err = primitive.ObjectIDFromHex(parts[1])
	if err != nil {
		return centerID, spaceID, false
	}
	return centerID, spaceID, true
}

func parseEntry(key string, centerID, spaceID primitive.ObjectID, id, raw string) (occupancy, bool) {
	// Formato del hash: start|end|state|token|owner|expires|bookingId|version
	f := strings.Split(raw, "|")
	if len(f) != 8 {
		return occupancy{}, false
	}
	startMs, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil {
		return occupancy{}, false
	}
	endMs, err := strconv.ParseInt(f[1], 10, 64)
	if err != nil || endMs <= startMs {
		return occupancy{}, false
	}
	e := occupancy{
		key:      key,
		centerID: centerID,
		spaceID:  spaceID,
		id:       id,
		start:    time.UnixMilli(startMs).UTC(),
		end:      time.UnixMilli(endMs).UTC(),
		state:    f[2],
		booking:  f[6],
	}
	if ms, err := strconv.ParseInt(f[5], 10, 64); err == nil && ms > 0 {
		e.expires = time.UnixMilli(ms).UTC()
	}
	if e.booking != "" {
		if _, err := primitive.ObjectIDFromHex(e.booking); err != nil {
			return occupancy{}, false
		}
	}
	return e, true
}

func loadCenters(ctx context.Context, db *mongo.Database) map[primitive.ObjectID]string {
	cursor, err := db.Collection("sport_centers").Find(ctx, bson.M{},
		options.Find().SetProjection(bson.M{"timezone": 1}))
	if err != nil {
		log.Fatalf("find sport_centers: %v", err)
	}
	var docs []struct {
		ID       primitive.ObjectID `bson:"_id"`
		Timezone string             `bson:"timezone"`
	}
	if err := cursor.All(ctx, &docs); err != nil {
		log.Fatalf("decode sport_centers: %v", err)
	}
	out := make(map[primitive.ObjectID]string, len(docs))
	for _, d := range docs {
		out[d.ID] = d.Timezone
	}
	return out
}

func loadSpaces(ctx context.Context, db *mongo.Database) map[primitive.ObjectID]primitive.ObjectID {
	cursor, err := db.Collection("spaces").Find(ctx, bson.M{},
		options.Find().SetProjection(bson.M{"sport_center_id": 1}))
	if err != nil {
		log.Fatalf("find spaces: %v", err)
	}
	var docs []struct {
		ID            primitive.ObjectID `bson:"_id"`
		SportCenterID primitive.ObjectID `bson:"sport_center_id"`
	}
	if err := cursor.All(ctx, &docs); err != nil {
		log.Fatalf("decode spaces: %v", err)
	}
	out := make(map[primitive.ObjectID]primitive.ObjectID, len(docs))
	for _, d := range docs {
		out[d.ID] = d.SportCenterID
	}
	return out
}

func knownNamespace(centerID, spaceID primitive.ObjectID, centers map[primitive.ObjectID]string, spaces map[primitive.ObjectID]primitive.ObjectID) bool {
	if _, ok := centers[centerID]; !ok {
		return false
	}
	owner, ok := spaces[spaceID]
	return ok && owner == centerID
}

func loadBookings(ctx context.Context, db *mongo.Database, entries []occupancy) map[string]bookingRef {
	seen := map[string]struct{}{}
	var ids []primitive.ObjectID
	for _, e := range entries {
		if e.booking == "" {
			continue
		}
		if _, ok := seen[e.booking]; ok {
			continue
		}
		seen[e.booking] = struct{}{}
		oid, err := primitive.ObjectIDFromHex(e.booking)
		if err != nil {
			continue
		}
		ids = append(ids, oid)
	}
	out := make(map[string]bookingRef, len(ids))
	const batch = 500
	for start := 0; start < len(ids); start += batch {
		end := start + batch
		if end > len(ids) {
			end = len(ids)
		}
		cursor, err := db.Collection("bookings").Find(ctx,
			bson.M{"_id": bson.M{"$in": ids[start:end]}},
			options.Find().SetProjection(bson.M{"occupancy_id": 1}))
		if err != nil {
			log.Fatalf("find bookings: %v", err)
		}
		var docs []struct {
			ID          primitive.ObjectID `bson:"_id"`
			OccupancyID string             `bson:"occupancy_id"`
		}
		if err := cursor.All(ctx, &docs); err != nil {
			log.Fatalf("decode bookings: %v", err)
		}
		for _, d := range docs {
			out[d.ID.Hex()] = bookingRef{occupancyID: d.OccupancyID}
		}
	}
	return out
}

func timezone(name string) *time.Location {
	if name == "" {
		name = defaultTimezone
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		loc, _ = time.LoadLocation(defaultTimezone)
	}
	return loc
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
