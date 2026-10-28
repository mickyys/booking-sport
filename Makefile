.PHONY: build vet test check audit-indexes audit-bookings audit-inventory test-redis test-redis-outage

MONGO_URI ?= mongodb://localhost:27017
MONGO_DB ?= sport_booking
SAMPLE_LIMIT ?= 10

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

# Verificación previa a commit/PR.
check: build vet test

# Lista índices reales y reporta anomalías conocidas (sólo lectura).
# STRICT=true devuelve exit 1 si hay anomalías.
audit-indexes:
	MONGO_URI=$(MONGO_URI) MONGO_DB=$(MONGO_DB) STRICT=$(STRICT) go run ./cmd/audit-indexes

# Inventario temporal de reservas (sólo lectura).
audit-bookings:
	MONGO_URI=$(MONGO_URI) MONGO_DB=$(MONGO_DB) SAMPLE_LIMIT=$(SAMPLE_LIMIT) go run ./cmd/audit-bookings

# Genera el inventario versionable de la migración UTC.
audit-inventory:
	MONGO_URI=$(MONGO_URI) MONGO_DB=$(MONGO_DB) SAMPLE_LIMIT=$(SAMPLE_LIMIT) \
		OUTPUT_MARKDOWN=../docs/migration-inventory.md \
		OUTPUT_FILE=/tmp/migration-inventory.json \
		go run ./cmd/audit-bookings

# Pruebas del coordinador de ocupación contra Redis local (docker compose up -d redis).
test-redis:
	REDIS_URL=$${REDIS_URL:-redis://localhost:6379} go test -tags integration ./internal/infra/redis/ -v -count=1

# Detiene y reinicia el Redis local para simular una caída (requiere docker compose).
test-redis-outage:
	REDIS_CONTAINER=booking_sport_redis REDIS_URL=$${REDIS_URL:-redis://localhost:6379} go test -tags integration ./internal/infra/redis/ -run TestOutageAndRecovery -v -count=1
