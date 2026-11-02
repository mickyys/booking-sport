// Command migrate-mp-connection-type agrega `mercadopago.connection_type` a
// sport_centers para distinguir las credenciales de Mercado Pago obtenidas por
// OAuth (Mercado Pago Connect) de los access tokens que el centro compartió a
// mano.
//
// Motivo: sólo una conexión OAuth autoriza a la plataforma a retener comisión
// en el split, así que sólo a esos centros se les puede enviar
// `marketplace_fee`. Con un token manual Mercado Pago rechaza la preferencia.
//
// Clasificación (sólo con evidencia, nunca se asume):
//   - mercadopago.refresh_token no vacío u mercadopago.connected_at presente
//     -> oauth (esas credenciales sólo existen si el alta fue por OAuth).
//   - mercadopago.access_token no vacío y sin evidencia de OAuth -> manual.
//   - sin access_token -> se omite (configuración vacía o desconectada).
//
// El campo es opcional y el backend infiere el tipo cuando falta
// (EffectiveConnectionType), así que el deploy no depende de esta migración.
//
// DRY_RUN por defecto:
//
//	MONGO_URI=... MONGO_DB=sport_booking go run ./cmd/migrate-mp-connection-type
//
// Aplicar:
//
//	APPLY=true MONGO_URI=... MONGO_DB=... go run ./cmd/migrate-mp-connection-type
//
// Revertir (sólo los documentos escritos por este comando):
//
//	ROLLBACK=true APPLY=true MONGO_URI=... MONGO_DB=... go run ./cmd/migrate-mp-connection-type
//
// Forzar un centro puntual (por si la inferencia se equivoca):
//
//	OVERRIDES=69b601e1989ae3a65d3e01b0=oauth,69e054444604771de4b6f745=manual
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/hamp/booking-sport/pkg/migratelog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	connectionOAuth  = "oauth"
	connectionManual = "manual"
	// migratedAtField marca los documentos escritos por esta migración para
	// que el rollback no toque los que cargó el flujo OAuth en runtime.
	migratedAtField = "mercadopago.connection_type_migrated_at"
)

func main() {
	defer migratelog.Start("migrate-mp-connection-type")()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	mongoURI := env("MONGO_URI", "mongodb://localhost:27017")
	dbName := env("MONGO_DB", "sport_booking")
	apply := env("APPLY", "false") == "true"
	rollback := env("ROLLBACK", "false") == "true"

	overrides, err := parseOverrides(os.Getenv("OVERRIDES"))
	if err != nil {
		log.Fatalf("OVERRIDES inválido: %v", err)
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(context.Background())
	centers := client.Database(dbName).Collection("sport_centers")

	mode := "DRY_RUN"
	if apply {
		mode = "APPLY"
	}

	if rollback {
		log.Printf("[MP-CONN] ROLLBACK mode=%s db=%s", mode, dbName)
		filter := bson.M{migratedAtField: bson.M{"$exists": true}}
		n, _ := centers.CountDocuments(ctx, filter)
		log.Printf("[MP-CONN] %d centro(s) a revertir", n)
		if apply {
			res, err := centers.UpdateMany(ctx, filter, bson.M{"$unset": bson.M{
				"mercadopago.connection_type":             "",
				"mercadopago.connection_type_migrated_at": "",
			}})
			if err != nil {
				log.Fatalf("rollback: %v", err)
			}
			log.Printf("[MP-CONN] revertidos=%d", res.ModifiedCount)
		}
		return
	}

	cur, err := centers.Find(ctx, bson.M{
		"mercadopago.access_token":    bson.M{"$nin": bson.A{"", nil}},
		"mercadopago.connection_type": bson.M{"$in": bson.A{"", nil}},
	})
	if err != nil {
		log.Fatalf("find: %v", err)
	}
	defer cur.Close(ctx)

	log.Printf("[MP-CONN] mode=%s db=%s overrides=%v", mode, dbName, overrides)
	counts := map[string]int{}
	now := time.Now().UTC()

	for cur.Next(ctx) {
		var raw bson.M
		if err := cur.Decode(&raw); err != nil {
			log.Fatalf("decode: %v", err)
		}
		id := raw["_id"].(primitive.ObjectID)
		name, _ := raw["name"].(string)

		mp, _ := raw["mercadopago"].(bson.M)
		hasRefresh := nonEmpty(mp, "refresh_token")
		hasConnectedAt := mp["connected_at"] != nil
		hasUserID := nonEmpty(mp, "mp_user_id")

		connectionType, reason := classify(hasRefresh, hasConnectedAt, hasUserID)
		forced, isOverride := overrides[id.Hex()]
		if isOverride {
			connectionType, reason = forced, "decisión del administrador"
		}
		counts[connectionType]++
		log.Printf("[MP-CONN] %s %-32s -> %-6s (refresh=%v connected_at=%v mp_user_id=%v; %s)",
			id.Hex(), name, connectionType, hasRefresh, hasConnectedAt, hasUserID, reason)

		if !apply {
			continue
		}
		if _, err := centers.UpdateOne(ctx,
			bson.M{"_id": id, "mercadopago.connection_type": bson.M{"$in": bson.A{"", nil}}},
			bson.M{"$set": bson.M{
				"mercadopago.connection_type":             connectionType,
				"mercadopago.connection_type_migrated_at": now,
			}}); err != nil {
			log.Fatalf("update %s: %v", id.Hex(), err)
		}
	}

	log.Printf("[MP-CONN] resumen=%v", counts)
	if !apply {
		log.Printf("[MP-CONN] DRY_RUN: no se escribió nada. Repetir con APPLY=true.")
	}
}

// classify decide el tipo de conexión a partir de las credenciales guardadas.
func classify(hasRefresh, hasConnectedAt, hasUserID bool) (string, string) {
	switch {
	case hasRefresh:
		return connectionOAuth, "tiene refresh_token (alta por OAuth)"
	case hasConnectedAt:
		return connectionOAuth, "tiene connected_at (alta por OAuth)"
	case hasUserID:
		return connectionOAuth, "tiene mp_user_id (alta por OAuth)"
	default:
		return connectionManual, "token compartido sin evidencia de OAuth"
	}
}

// parseOverrides interpreta "id=oauth,id2=manual".
func parseOverrides(raw string) (map[string]string, error) {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("%q: se esperaba id=oauth|manual", part)
		}
		if _, err := primitive.ObjectIDFromHex(kv[0]); err != nil {
			return nil, fmt.Errorf("%q: id inválido", kv[0])
		}
		if kv[1] != connectionOAuth && kv[1] != connectionManual {
			return nil, fmt.Errorf("%q: tipo debe ser oauth o manual", kv[1])
		}
		out[kv[0]] = kv[1]
	}
	return out, nil
}

func nonEmpty(m bson.M, key string) bool {
	s, _ := m[key].(string)
	return s != ""
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
