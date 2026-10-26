// Command remove-fintoc-credentials elimina de sport_centers las credenciales
// de Fintoc (campo `fintoc`: llaves y secretos de webhook en texto plano) y la
// marca commercial.online_payment.retired_providers que dejaba la migración
// comercial. No toca Mercado Pago ni ningún otro campo.
//
// Por seguridad NO copia los secretos a otra colección ni los escribe en el log
// (solo registra los nombres de las claves eliminadas). El respaldo debe ser un
// mongodump externo previo; el comando no tiene rollback.
//
// DRY_RUN por defecto:
//
//	MONGO_URI=... MONGO_DB=sport_booking go run ./cmd/remove-fintoc-credentials
//
// Aplicar:
//
//	APPLY=true MONGO_URI=... go run ./cmd/remove-fintoc-credentials
package main

import (
	"context"
	"log"
	"os"
	"sort"
	"time"

	"github.com/hamp/booking-sport/pkg/migratelog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	defer migratelog.Start("remove-fintoc-credentials")()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	apply := env("APPLY", "false") == "true"
	mode := "DRY_RUN"
	if apply {
		mode = "APPLY"
	}
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(env("MONGO_URI", "mongodb://localhost:27017")))
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database(env("MONGO_DB", "sport_booking"))
	centers := db.Collection("sport_centers")

	filter := bson.M{"$or": bson.A{
		bson.M{"fintoc": bson.M{"$exists": true}},
		bson.M{"commercial.online_payment.retired_providers": bson.M{"$exists": true}},
	}}
	cur, err := centers.Find(ctx, filter, options.Find().SetProjection(bson.M{"name": 1, "fintoc": 1, "commercial.online_payment.retired_providers": 1}))
	if err != nil {
		log.Fatalf("find: %v", err)
	}
	var docs []bson.M
	if err := cur.All(ctx, &docs); err != nil {
		log.Fatalf("decode: %v", err)
	}
	log.Printf("[FINTOC-CREDS] mode=%s centros=%d", mode, len(docs))

	for _, d := range docs {
		paths := keyPaths("fintoc", d["fintoc"])
		_, hasMarker := lookup(d, "commercial", "online_payment", "retired_providers")
		log.Printf("[FINTOC-CREDS] %v %v claves=%v marca_retired=%v", d["_id"], d["name"], paths, hasMarker)
		if !apply {
			continue
		}
		if _, err := centers.UpdateOne(ctx, bson.M{"_id": d["_id"]}, bson.M{
			"$unset": bson.M{"fintoc": "", "commercial.online_payment.retired_providers": ""},
			"$set":   bson.M{"updated_at": time.Now().UTC()},
		}); err != nil {
			log.Fatalf("update %v: %v", d["_id"], err)
		}
	}

	if apply {
		left, err := centers.CountDocuments(ctx, filter)
		if err != nil || left != 0 {
			log.Fatalf("[FINTOC-CREDS] quedan %d centros con datos de Fintoc (err=%v)", left, err)
		}
		log.Printf("[FINTOC-CREDS] eliminado en %d centro(s); verificación OK", len(docs))
	} else {
		log.Printf("[FINTOC-CREDS] DRY_RUN: no se escribió nada. Hacer antes un mongodump y repetir con APPLY=true.")
	}
}

// keyPaths devuelve solo los nombres de las claves (nunca los valores).
func keyPaths(prefix string, v interface{}) []string {
	m, ok := v.(bson.M)
	if !ok {
		if v == nil {
			return nil
		}
		return []string{prefix}
	}
	var out []string
	for k, val := range m {
		out = append(out, keyPaths(prefix+"."+k, val)...)
	}
	sort.Strings(out)
	return out
}

func lookup(m bson.M, path ...string) (interface{}, bool) {
	var cur interface{} = m
	for _, p := range path {
		mm, ok := cur.(bson.M)
		if !ok {
			return nil, false
		}
		cur, ok = mm[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
