// Command migrate-commercial-model agrega el subdocumento `commercial` a
// sport_centers (ver docs/refactor-modelo-comercial.md, sección 7 y 8).
//
// Sólo AGREGA el campo `commercial`; no modifica reservas, espacios, horarios,
// precios ni credenciales (Mercado Pago, Fintoc), y no cambia `status`. Sólo
// cambia `payment_mode` en los centros indicados en OVERRIDES. Es idempotente:
// centros con `commercial` ya existente se omiten.
//
// Decisiones (docs/plan-migracion-modelo-comercial.md):
//   - Comisión de centros migrados: DEFAULT_COMMISSION (0 por defecto), con
//     source=CENTER (0% explícito). Un fee propio > 0 se conserva.
//   - PAY_AT_CENTER: suscripción ACTIVE, plan LEGACY, monto 0.
//   - Fintoc se marca RETIRED en online_payment.retired_providers; no se borra.
//   - OVERRIDES=<id>=<PAY_ONLINE|PAY_AT_CENTER>[,<id>=...] fuerza la modalidad
//     por decisión del administrador y fija `payment_mode` legacy.
//
// Clasificación (sólo con evidencia, nunca se asume):
//   - payment_mode explícito (pay_online / pay_at_center) -> se respeta.
//   - sin payment_mode, con credenciales MP y reservas mercadopago
//     confirmadas -> PAY_ONLINE.
//   - sin payment_mode y sin credenciales MP, con reservas -> PAY_AT_CENTER.
//   - cualquier otro caso (p. ej. MP conectado pero sin cobros online) ->
//     NEEDS_REVIEW (revisión administrativa).
//
// DRY_RUN por defecto:
//
//	MONGO_URI=... MONGO_DB=sport_booking_prod go run ./cmd/migrate-commercial-model
//
// Aplicar:
//
//	APPLY=true MONGO_URI=... MONGO_DB=... go run ./cmd/migrate-commercial-model
//
// Revertir (sólo centros migrados por este comando):
//
//	ROLLBACK=true APPLY=true MONGO_URI=... MONGO_DB=... go run ./cmd/migrate-commercial-model
//
// Opciones: DEFAULT_COMMISSION=0 OVERRIDES=69e054444604771de4b6f745=PAY_AT_CENTER
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hamp/booking-sport/pkg/migratelog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const migrationTag = "commercial-model-v1"

func main() {
	defer migratelog.Start("migrate-commercial-model")()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	mongoURI := env("MONGO_URI", "mongodb://localhost:27017")
	dbName := env("MONGO_DB", "sport_booking")
	apply := env("APPLY", "false") == "true"
	rollback := env("ROLLBACK", "false") == "true"
	defaultCommission, err := strconv.Atoi(env("DEFAULT_COMMISSION", "0"))
	if err != nil || defaultCommission < 0 || defaultCommission > 100 {
		log.Fatalf("DEFAULT_COMMISSION inválida (entero 0-100): %v", err)
	}
	overrides, err := parseOverrides(os.Getenv("OVERRIDES"))
	if err != nil {
		log.Fatalf("OVERRIDES inválido: %v", err)
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database(dbName)
	centers := db.Collection("sport_centers")

	mode := "DRY_RUN"
	if apply {
		mode = "APPLY"
	}

	if rollback {
		log.Printf("[COMMERCIAL] ROLLBACK mode=%s db=%s", mode, dbName)
		filter := bson.M{"commercial.migration": migrationTag}
		n, _ := centers.CountDocuments(ctx, filter)
		log.Printf("[COMMERCIAL] %d centro(s) a revertir", n)
		if apply {
			// Primero se revierte el payment_mode legacy fijado por la migración.
			if _, err := centers.UpdateMany(ctx,
				bson.M{"commercial.migration": migrationTag, "commercial.legacy_payment_mode_set": true},
				bson.M{"$unset": bson.M{"payment_mode": ""}}); err != nil {
				log.Fatalf("rollback payment_mode: %v", err)
			}
			res, err := centers.UpdateMany(ctx, filter, bson.M{"$unset": bson.M{"commercial": ""}})
			if err != nil {
				log.Fatalf("rollback: %v", err)
			}
			log.Printf("[COMMERCIAL] revertidos=%d", res.ModifiedCount)
		}
		return
	}

	cur, err := centers.Find(ctx, bson.M{"commercial": bson.M{"$exists": false}})
	if err != nil {
		log.Fatalf("find: %v", err)
	}
	defer cur.Close(ctx)

	log.Printf("[COMMERCIAL] mode=%s db=%s defaultCommission=%d overrides=%v", mode, dbName, defaultCommission, overrides)
	counts := map[string]int{}
	for cur.Next(ctx) {
		var raw bson.M
		if err := cur.Decode(&raw); err != nil {
			log.Fatalf("decode: %v", err)
		}
		id := raw["_id"].(primitive.ObjectID)
		name, _ := raw["name"].(string)

		hasMP := hasNonEmpty(raw, "mercadopago", "access_token")
		explicit, _ := raw["payment_mode"].(string)
		mpPaid := countBookings(ctx, db, id, bson.M{"payment_method": "mercadopago", "status": "confirmed"})
		anyBookings := countBookings(ctx, db, id, bson.M{})

		paymentMode, reason := classify(explicit, hasMP, mpPaid, anyBookings)
		forced, isOverride := overrides[id.Hex()]
		if isOverride {
			paymentMode, reason = forced, "decisión del administrador"
		}
		counts[paymentMode]++
		log.Printf("[COMMERCIAL] %s %-30s -> %-13s (%s; mp=%v mpPaid=%d bookings=%d)",
			id.Hex(), name, paymentMode, reason, hasMP, mpPaid, anyBookings)

		commercial := bson.M{
			"payment_mode": paymentMode,
			"migration":    migrationTag,
			"migrated_at":  time.Now().UTC(),
			"review":       paymentMode == "NEEDS_REVIEW",
			"reason":       reason,
		}
		switch paymentMode {
		case "PAY_ONLINE":
			rate := int32(defaultCommission)
			if v, ok := toFloat(raw["marketplace_fee_percent"]); ok && v > 0 && v <= 100 {
				rate = int32(v)
			}
			status := "NOT_CONNECTED"
			if hasMP {
				status = "CONNECTED"
			}
			online := bson.M{
				"commission":       bson.M{"type": "PERCENTAGE", "value": rate, "source": "CENTER"},
				"payment_provider": bson.M{"provider": "MERCADO_PAGO", "status": status},
			}
			if _, ok := raw["fintoc"]; ok {
				online["retired_providers"] = bson.A{bson.M{"provider": "FINTOC", "status": "RETIRED"}}
			}
			commercial["online_payment"] = online
		case "PAY_AT_CENTER":
			commercial["subscription"] = bson.M{
				"status":     "ACTIVE",
				"plan":       "LEGACY",
				"amount":     0,
				"started_at": time.Now().UTC(),
			}
		}
		set := bson.M{"commercial": commercial}
		if isOverride && paymentMode != "NEEDS_REVIEW" {
			legacy := map[string]string{"PAY_ONLINE": "pay_online", "PAY_AT_CENTER": "pay_at_center"}[paymentMode]
			if explicit != legacy {
				set["payment_mode"] = legacy
				commercial["legacy_payment_mode_set"] = true
			}
		}

		if !apply {
			continue
		}
		_, err := centers.UpdateOne(ctx,
			bson.M{"_id": id, "commercial": bson.M{"$exists": false}},
			bson.M{"$set": set})
		if err != nil {
			log.Fatalf("update %s: %v", id.Hex(), err)
		}
	}
	log.Printf("[COMMERCIAL] resumen=%v", counts)
	if !apply {
		log.Printf("[COMMERCIAL] DRY_RUN: no se escribió nada. Repetir con APPLY=true.")
	}
}

// parseOverrides interpreta "id=MODE,id2=MODE".
func parseOverrides(raw string) (map[string]string, error) {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("%q: se esperaba id=MODE", part)
		}
		if _, err := primitive.ObjectIDFromHex(kv[0]); err != nil {
			return nil, fmt.Errorf("%q: id inválido", kv[0])
		}
		if kv[1] != "PAY_ONLINE" && kv[1] != "PAY_AT_CENTER" {
			return nil, fmt.Errorf("%q: modalidad debe ser PAY_ONLINE o PAY_AT_CENTER", kv[1])
		}
		out[kv[0]] = kv[1]
	}
	return out, nil
}

func classify(explicit string, hasMP bool, mpPaid, bookings int64) (string, string) {
	switch explicit {
	case "pay_online":
		return "PAY_ONLINE", "payment_mode explícito"
	case "pay_at_center":
		return "PAY_AT_CENTER", "payment_mode explícito"
	}
	switch {
	case hasMP && mpPaid > 0:
		return "PAY_ONLINE", "MP conectado y cobros online confirmados"
	case !hasMP && bookings > 0:
		return "PAY_AT_CENTER", "sin MP, reservas sin cobro online"
	case hasMP && mpPaid == 0:
		return "NEEDS_REVIEW", "MP conectado pero sin cobros online"
	default:
		return "NEEDS_REVIEW", "información insuficiente"
	}
}

func countBookings(ctx context.Context, db *mongo.Database, center primitive.ObjectID, extra bson.M) int64 {
	f := bson.M{"sport_center_id": center}
	for k, v := range extra {
		f[k] = v
	}
	n, err := db.Collection("bookings").CountDocuments(ctx, f)
	if err != nil {
		log.Fatalf("count bookings: %v", err)
	}
	return n
}

func hasNonEmpty(m bson.M, parent, key string) bool {
	sub, ok := m[parent].(bson.M)
	if !ok {
		return false
	}
	s, _ := sub[key].(string)
	return s != ""
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
