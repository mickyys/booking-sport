#!/usr/bin/env bash
#
# Migración de datos a producción: modelo comercial + spaces/schedule_rules.
# Guía completa y rollbacks: docs/runbook-migracion-produccion.md
#
# Uso:
#   export MONGO_URI='mongodb+srv://<usuario>:<clave>@<cluster>/'
#   export MONGO_DB=sport_booking
#
#   scripts/migrate-prod.sh                      # dry-run de todos los pasos
#   CONFIRM=migrar-produccion MODE=apply scripts/migrate-prod.sh
#
# Requisitos antes de MODE=apply (ver §1 del runbook):
#   - snapshot de Atlas + mongodump verificado (restaurado en otro lado)
#   - app en pausa y backend nuevo listo para desplegar en la misma ventana
#   - BOOKING_CODES / RECURRING_IDS definidos con el dry-run de duplicados
#   - COMMERCIAL_OVERRIDES con los centros que van a PAY_AT_CENTER
#
# Variables:
#   MONGO_URI / MONGO_DB      obligatorias
#   MODE                      dry-run (por defecto) | apply
#   CONFIRM                   debe ser "migrar-produccion" para MODE=apply
#   BOOKING_CODES             códigos de reservas duplicadas a cancelar
#   RECURRING_IDS             ids de recurrentes duplicadas a cancelar
#   DEFAULT_COMMISSION        comisión por defecto (porcentaje), por defecto 0
#   COMMERCIAL_OVERRIDES      id=PAY_AT_CENTER[,id2=...] para el modelo comercial (alias: OVERRIDES)
#   MP_OVERRIDES              id=oauth|manual para migrate-mp-connection-type
#   MIGRATE_LOG_DIR           logs de los comandos (por defecto ./logs/migrations)
#   BACKUP_DIR                dumps previos (por defecto $MIGRATE_LOG_DIR/backups)
#   EXPECTED_QUINCHOS         quinchos esperados (opcional; si se define, se verifica)
#   SKIP_CHECKS=1             no ejecutar las verificaciones con mongosh
#   FORCE_SPACES=1            permitir migrate-spaces con spaces ya existentes
#
# Este script NO despliega el backend ni genera recurrentes: terminado el bloque
# de datos, seguir con §4 del runbook (deploy + migrate-recurring-bookings).
set -euo pipefail

cd "$(dirname "$0")/.."

MODE="${MODE:-dry-run}"
MIGRATE_LOG_DIR="${MIGRATE_LOG_DIR:-./logs/migrations}"
BACKUP_DIR="${BACKUP_DIR:-$MIGRATE_LOG_DIR/backups}"
DEFAULT_COMMISSION="${DEFAULT_COMMISSION:-0}"
# OJO: migrate-commercial-model usa OVERRIDES=centro=PAY_AT_CENTER|PAY_ONLINE y
# migrate-mp-connection-type usa OVERRIDES=centro=oauth|manual. Son incompatibles:
# el script los pasa por separado para no mezclarlos.
COMMERCIAL_OVERRIDES="${COMMERCIAL_OVERRIDES:-${OVERRIDES:-}}"
MP_OVERRIDES="${MP_OVERRIDES:-}"

die() { echo "ERROR: $*" >&2; exit 1; }
info() { echo; echo "==> $*"; }
ok() { echo "    OK: $*"; }

[ -n "${MONGO_URI:-}" ] || die "falta MONGO_URI (no se lee de .env a propósito)"
[ -n "${MONGO_DB:-}" ] || die "falta MONGO_DB (ej: sport_booking)"
[ "$MODE" = "dry-run" ] || [ "$MODE" = "apply" ] || die "MODE debe ser dry-run o apply"

case "$MODE" in
  apply)
    [ "${CONFIRM:-}" = "migrar-produccion" ] || die \
      "MODE=apply requiere CONFIRM=migrar-produccion. Antes: app en pausa, snapshot + mongodump verificados y backend nuevo listo."
    [ -n "${BOOKING_CODES:-}${RECURRING_IDS:-}" ] || die \
      "MODE=apply requiere BOOKING_CODES y/o RECURRING_IDS (salen del dry-run de duplicados)"
    ;;
esac

export MIGRATE_LOG_DIR
mkdir -p "$MIGRATE_LOG_DIR" "$BACKUP_DIR"

URI_BASE="${MONGO_URI%/}"

TS="$(date -u +%Y%m%d-%H%M%S)"
RUN_LOG="$MIGRATE_LOG_DIR/migrate-prod-$TS.log"

# Toda la salida a consola y a un log propio (los comandos dejan además su log [Migrate]).
exec > >(tee -a "$RUN_LOG") 2>&1

echo "Migración a producción — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "db=$MONGO_DB mode=$MODE default_commission=$DEFAULT_COMMISSION%"
echo "logs=$MIGRATE_LOG_DIR backups=$BACKUP_DIR run_log=$RUN_LOG"

# ---------------------------------------------------------------- utilidades

has_mongosh() { command -v mongosh >/dev/null 2>&1; }

# mongo_eval <js> -> imprime el resultado
mongo_eval() {
  has_mongosh || die "mongosh no está instalado (o usar SKIP_CHECKS=1)"
  mongosh "$URI_BASE/$MONGO_DB" --quiet --eval "$1"
}

check_or_die() {
  local desc="$1" js="$2" want="$3"
  [ "${SKIP_CHECKS:-0}" = "1" ] && { echo "    [skip check] $desc"; return 0; }
  local got
  got="$(mongo_eval "$js" | tail -1 | tr -d '[:space:]')"
  [ "$got" = "$want" ] || die "$desc: esperado $want, obtenido ${got:-<vacío>}"
  ok "$desc = $got"
}

run_cmd() {
  local name="$1"; shift
  echo
  echo "--- $name $*"
  env "$@" go run "./cmd/$name"
}

backup_collection() {
  local coll="$1"
  local file="$BACKUP_DIR/${coll}-pre-migracion-$TS.gz"
  command -v mongodump >/dev/null 2>&1 || die "mongodump no está instalado"
  echo "    respaldo $coll -> $file"
  mongodump --uri="$MONGO_URI" --db="$MONGO_DB" --collection="$coll" --gzip --archive="$file" >/dev/null
  [ -s "$file" ] || die "el respaldo de $coll quedó vacío"
}

# ------------------------------------------------------------- preflight

info "Preflight (solo lectura)"
CENTER_COUNT="$(mongo_eval 'print(db.sport_centers.countDocuments({}))')"
BOOKINGS="$(mongo_eval 'print(db.bookings.countDocuments({}))')"
COURTS="$(mongo_eval 'print(db.courts.countDocuments({}))')"
SPACES="$(mongo_eval 'print(db.spaces.countDocuments({}))')"
echo "    centros=$CENTER_COUNT bookings=$BOOKINGS courts=$COURTS spaces=$SPACES"

[ "$CENTER_COUNT" -ge 1 ] || die "no hay centros en $MONGO_DB: ¿MONGO_URI/MONGO_DB correctos?"

if [ "$MODE" = "apply" ]; then
  backup_collection bookings
fi

run_cmd audit-indexes
run_cmd audit-bookings

if [ "$MODE" = "dry-run" ]; then
  echo
  echo "==> Dry-run: se ejecutan solo los comandos que lo soportan."
  echo "    Se omiten migrate-spaces y migrate-geo: escriben siempre (no tienen dry-run)."
fi

# ------------------------------------------------------------------ paso 1

info "3.1 Reservas de Fintoc"
run_cmd remove-fintoc-bookings
if [ "$MODE" = "apply" ]; then
  run_cmd remove-fintoc-bookings APPLY=true
  check_or_die "reservas fintoc" 'print(db.bookings.countDocuments({payment_method:"fintoc"}))' 0
fi

# ------------------------------------------------------------------ paso 2

info "3.2 Duplicados"
if [ "$MODE" = "apply" ]; then
  run_cmd migrate-resolve-duplicates APPLY=true BOOKING_CODES="${BOOKING_CODES:-}" RECURRING_IDS="${RECURRING_IDS:-}"
  check_or_die "duplicados confirmados" 'print(db.bookings.aggregate([{$match:{status:"confirmed"}},{$group:{_id:{c:"$court_id",d:{$ifNull:["$local_date",{$dateToString:{format:"%Y-%m-%d",date:"$date",timezone:"America/Santiago"}}]},h:"$hour",m:"$minutes"},n:{$sum:1}}},{$match:{n:{$gt:1}}},{$count:"g"}]).toArray()[0]?.g||0)' 0
  check_or_die "duplicados recurrentes activos" 'print(db.recurring_reservations.aggregate([{$match:{status:"active"}},{$group:{_id:{c:"$court_id",d:"$day_of_week",h:"$hour"},n:{$sum:1}}},{$match:{n:{$gt:1}}},{$count:"g"}]).toArray()[0]?.g||0)' 0
elif [ -n "${BOOKING_CODES:-}${RECURRING_IDS:-}" ]; then
  run_cmd migrate-resolve-duplicates BOOKING_CODES="${BOOKING_CODES:-}" RECURRING_IDS="${RECURRING_IDS:-}"
else
  echo "    [omitido] definir BOOKING_CODES / RECURRING_IDS para revisar los duplicados"
fi

# ------------------------------------------------------------- pasos 3-4

info "3.3 Espacios y geolocalización"
if [ "$MODE" = "apply" ]; then
  if [ "$SPACES" -gt 0 ] && [ "${FORCE_SPACES:-0}" != "1" ]; then
    echo "    spaces ya tiene $SPACES documentos: se omite migrate-spaces."
    echo "    (volver a correrlo resetearía los quinchos a cancha; FORCE_SPACES=1 para forzarlo)"
  else
    run_cmd migrate-spaces
    check_or_die "spaces = courts" 'print(db.spaces.countDocuments({})===db.courts.countDocuments({})?1:0)' 1
  fi
  run_cmd migrate-geo
  check_or_die "centros con location" 'print(db.sport_centers.countDocuments({location:{$exists:false}}))' 0
else
  echo "    [omitido] migrate-spaces y migrate-geo escriben siempre (no tienen dry-run)"
fi

# ------------------------------------------------------------------ paso 5

info "3.4 Reglas de horario"
run_cmd migrate-schedule-rules DRY_RUN=true
if [ "$MODE" = "apply" ]; then
  run_cmd migrate-schedule-rules
  check_or_die "reglas de horario" 'print(db.schedule_rules.countDocuments({})>0?1:0)' 1
  check_or_die "franjas de duración cero" 'print(db.schedule_rules.aggregate([{$unwind:"$slots"},{$match:{$expr:{$eq:["$slots.start_time","$slots.end_time"]}}},{$count:"g"}]).toArray()[0]?.g||0)' 0
fi

# ------------------------------------------------------------------ paso 6

info "3.5 Zona horaria de los centros"
run_cmd migrate-sport-center-timezone
if [ "$MODE" = "apply" ]; then
  run_cmd migrate-sport-center-timezone APPLY=true
  check_or_die "centros sin timezone" 'print(db.sport_centers.countDocuments({$or:[{timezone:{$exists:false}},{timezone:""}]}))' 0
fi

# ------------------------------------------------------------------ paso 7

info "3.6 Campos temporales de las reservas (E11000 = quedan duplicados)"
run_cmd migrate-booking-schedule-fields
if [ "$MODE" = "apply" ]; then
  run_cmd migrate-booking-schedule-fields APPLY=true
  check_or_die "reservas sin scheduled_at" 'print(db.bookings.countDocuments({scheduled_at:{$exists:false}}))' 0
fi

# ------------------------------------------------------------------ paso 8

info "3.7 Quinchos por día completo"
run_cmd migrate-quincho-day
if [ "$MODE" = "apply" ]; then
  run_cmd migrate-quincho-day APPLY=true
  if [ -n "${EXPECTED_QUINCHOS:-}" ]; then
    check_or_die "espacios category=quincho" 'print(db.spaces.countDocuments({category:"quincho"}))' "$EXPECTED_QUINCHOS"
  fi
fi

# ------------------------------------------------------------------ paso 9

info "3.8 Control intermedio: audit-bookings debe dar 0 hallazgos"
if [ "$MODE" = "apply" ]; then
  audit_out="$(go run ./cmd/audit-bookings 2>&1)"
  echo "$audit_out" | tail -20
  findings="$(echo "$audit_out" | sed -n 's/.*Total de hallazgos:[[:space:]]*\([0-9]*\).*/\1/p' | tail -1)"
  [ "${findings:-x}" = "0" ] || die "audit-bookings reporta ${findings:-<sin dato>} hallazgos: no continuar"
  ok "audit-bookings = 0 hallazgos"
else
  run_cmd audit-bookings
fi

# ----------------------------------------------------------------- paso 10

info "3.9 Modelo comercial"
run_cmd migrate-commercial-model DEFAULT_COMMISSION="$DEFAULT_COMMISSION" OVERRIDES="$COMMERCIAL_OVERRIDES"
if [ "$MODE" = "apply" ]; then
  run_cmd migrate-commercial-model APPLY=true DEFAULT_COMMISSION="$DEFAULT_COMMISSION" OVERRIDES="$COMMERCIAL_OVERRIDES"
  check_or_die "centros sin payment_mode" 'print(db.sport_centers.countDocuments({$or:[{"commercial.payment_mode":{$exists:false}},{"commercial.payment_mode":""}]}))' 0
fi

# ----------------------------------------------------------------- paso 11

info "3.10 Credenciales de Fintoc (SIN rollback: se respalda sport_centers antes)"
run_cmd remove-fintoc-credentials
if [ "$MODE" = "apply" ]; then
  backup_collection sport_centers
  run_cmd remove-fintoc-credentials APPLY=true
  check_or_die "centros con campo fintoc" 'print(db.sport_centers.countDocuments({fintoc:{$exists:true}}))' 0
fi

# ----------------------------------------------------------------- paso 12

info "3.11 Tipo de conexión de Mercado Pago"
run_cmd migrate-mp-connection-type OVERRIDES="$MP_OVERRIDES"
if [ "$MODE" = "apply" ]; then
  run_cmd migrate-mp-connection-type APPLY=true OVERRIDES="$MP_OVERRIDES"
  check_or_die "conexiones MP sin clasificar" 'print(db.sport_centers.countDocuments({"mercadopago.access_token":{$exists:true,$ne:""},"mercadopago.connection_type":{$exists:false}}))' 0
fi

# ------------------------------------------------------------- verificación

info "Verificación final"
mongo_eval '
const b = db.bookings.countDocuments({});
print("bookings:", b);
print("  fintoc:", db.bookings.countDocuments({payment_method:"fintoc"}));
print("  sin scheduled_at:", db.bookings.countDocuments({scheduled_at:{$exists:false}}));
print("  sin local_date:", db.bookings.countDocuments({local_date:{$exists:false}}));
print("spaces:", db.spaces.countDocuments({}), "| canchas:", db.spaces.countDocuments({category:"cancha"}), "| quinchos:", db.spaces.countDocuments({category:"quincho"}));
print("schedule_rules:", db.schedule_rules.countDocuments({}));
print("recurrentes activas:", db.recurring_reservations.countDocuments({status:"active"}));
print("migration_backups:", db.migration_backups.countDocuments({}));
db.sport_centers.find({}, {slug:1, courts_count:1, timezone:1, "commercial.payment_mode":1, "mercadopago.connection_type":1})
  .forEach(c => print("centro", c.slug, "courts_count="+c.courts_count, "tz="+(c.timezone||"-"), "mode="+(c.commercial&&c.commercial.payment_mode||"-"), "mp="+(c.mercadopago&&c.mercadopago.connection_type||"-")));
'

echo
if [ "$MODE" = "apply" ]; then
  cat <<'EOT'
Datos migrados. Faltan los pasos manuales (§4 del runbook):
  1. Desplegar el backend nuevo con COMMERCIAL_MODEL_ENFORCED=false.
     Verificar en el log: "commercial_model ... enforced=false" y "All indexes created successfully".
  2. Revisar audit-indexes: debe bajar a 4 hallazgos (los de `users`).
  3. Generar recurrentes:
       go run ./cmd/migrate-recurring-bookings
       APPLY=true API_BASE_URL=https://<backend> RECURRING_JOB_TOKEN=<token> go run ./cmd/migrate-recurring-bookings
  4. Si BOOKING_REDIS_AUTHORITY=true: reconcile-occupancy con MODE=import y luego MODE=reconcile.
  5. Levantar la app y habilitar el workflow diario (06:00 UTC).
  6. Revocar llaves de Fintoc y rotar los tokens de Mercado Pago que estuvieron en texto plano.
EOT
else
  echo "Dry-run terminado. Nada se escribió (migrate-spaces/migrate-geo se omitieron a propósito)."
fi
echo "Log de esta corrida: $RUN_LOG"
