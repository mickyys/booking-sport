# Runbook: migración a producción (modelo comercial)

Diseño y decisiones: `docs/plan-migracion-modelo-comercial.md`. Este documento es la guía de ejecución en producción. Todo se probó antes en la copia local `sport_booking`; el ensayo con datos frescos (punto 1) es obligatorio.

Reglas generales:
- Cada comando es dry-run por defecto; aplicar con `APPLY=true` solo después de revisar la salida.
- Cada comando deja un log con tag `[Migrate]` en `logs/migrations/` (un archivo por ejecución). Un archivo **sin línea `FIN`** es una ejecución fallida. Conservar todos los archivos como evidencia.
- No cargar el `.env` de Docker (`MONGO_URI=mongodb://mongodb:27017`): exportar `MONGO_URI` explícitamente.
- No registrar ni pegar tokens, contraseñas ni la URI completa en chats o tickets.

## 0. Variables

```bash
cd booking-sport
export MONGO_URI='mongodb+srv://<usuario-migracion>:<clave>@<cluster>/'   # usuario con escritura solo para la migración
export MONGO_DB=sport_booking
export MIGRATE_LOG_DIR=./logs/migrations
```

## 1. Antes de la ventana

- [ ] **Ensayo con datos frescos.** Restaurar el último snapshot de Atlas en un Mongo local y correr las secciones 3 y 4 completas. Con ese ensayo definir:
  - `BOOKING_CODES` y `RECURRING_IDS` de duplicados (los de la copia del 25-10 pueden haber cambiado: ejecutar el dry-run de `migrate-resolve-duplicates` y revisar con el centro cuál conservar).
  - Cantidad real de reservas de Fintoc y de recurrentes activas.
  - Que `migrate-booking-schedule-fields` no falle con `E11000` (señal de duplicados sin resolver).
- [ ] **Accesos.** Rotar la contraseña de Atlas (se compartió por chat). Usuario de solo lectura para dumps y usuario con escritura solo para la migración. IP de la máquina en la lista de acceso de Atlas.
- [ ] **Respaldo.** Snapshot de Atlas + `mongodump` de `sport_booking`, y comprobar que se puede restaurar.
- [ ] **Variables del backend nuevo** (sin activar el modelo):
  - `COMMERCIAL_MODEL_ENFORCED=false`
  - `RECURRING_JOB_TOKEN` (igual al secreto de GitHub)
  - Si habrá suscripciones: `SUBSCRIPTION_MONTHLY_AMOUNT`, `SUBSCRIPTION_BACK_URL`, `MP_ACCESS_TOKEN` de plataforma
- [ ] **GitHub Actions** (`booking-sport-batch`): variable `API_BASE_URL` y secreto `RECURRING_JOB_TOKEN`.
- [ ] **Redis.** Revisar si `BOOKING_REDIS_AUTHORITY=true` en producción. Si lo está, correr `reconcile-occupancy` al final (cambian horarios de quinchos y se crean reservas).
- [ ] **Ventana de mantenimiento.** El backend actual lee `courts`, no `spaces`: no puede servir los datos migrados. Migrar y desplegar el backend nuevo juntos, con la app en pausa.
- [ ] Avisar al administrador de Catolica (duplicados, quinchos por día completo, descripción de los quinchos).

## 2. Línea base (dentro de la ventana, app en pausa)

```bash
go run ./cmd/audit-indexes
go run ./cmd/audit-bookings
```

Las 4 anomalías de índices de `users` son preexistentes y no bloquean. Guardar la salida.

## 3. Migración de datos (en este orden)

Para cada comando: ejecutar sin `APPLY`, revisar, ejecutar con `APPLY=true` cuando aplique.

```bash
# 3.1 Reservas de Fintoc (aborta si hay futuras confirmadas)
go run ./cmd/remove-fintoc-bookings
APPLY=true go run ./cmd/remove-fintoc-bookings

# 3.2 Duplicados (valores definidos en el ensayo)
export BOOKING_CODES=633c7e11,3a46b3e1,b5427ce4
export RECURRING_IDS=69e6ca4e5526854c22ccd068
go run ./cmd/migrate-resolve-duplicates
APPLY=true go run ./cmd/migrate-resolve-duplicates
unset BOOKING_CODES RECURRING_IDS

# 3.3 Espacios y geolocalización
go run ./cmd/migrate-spaces
go run ./cmd/migrate-geo

# 3.4 Reglas de horario (sin FORCE)
DRY_RUN=true go run ./cmd/migrate-schedule-rules
go run ./cmd/migrate-schedule-rules

# 3.5 Zona horaria de los centros
go run ./cmd/migrate-sport-center-timezone
APPLY=true go run ./cmd/migrate-sport-center-timezone

# 3.6 Campos temporales de las reservas (E11000 = quedan duplicados)
go run ./cmd/migrate-booking-schedule-fields
APPLY=true go run ./cmd/migrate-booking-schedule-fields

# 3.7 Quinchos por día completo (siempre después de 3.3 a 3.6)
go run ./cmd/migrate-quincho-day
APPLY=true go run ./cmd/migrate-quincho-day

# 3.8 Control intermedio: debe dar 0 hallazgos
go run ./cmd/audit-bookings

# 3.9 Modelo comercial
export DEFAULT_COMMISSION=0 OVERRIDES=69e054444604771de4b6f745=PAY_AT_CENTER
go run ./cmd/migrate-commercial-model
APPLY=true go run ./cmd/migrate-commercial-model
unset DEFAULT_COMMISSION OVERRIDES

# 3.10 Credenciales de Fintoc: SIN rollback. Antes, mongodump de sport_centers.
go run ./cmd/remove-fintoc-credentials
APPLY=true go run ./cmd/remove-fintoc-credentials
```

Comprobaciones por paso: spaces = cantidad de `courts`; 0 centros sin timezone; 0 reservas sin `scheduled_at`; 0 hallazgos en `audit-bookings`; `courts_count` de cada centro = número de canchas (los quinchos ya no cuentan).

## 4. Despliegue y recurrentes

1. Desplegar el backend nuevo (flag apagado). En el log de arranque debe verse `commercial_model ... enforced=false`.
2. Generar las primeras 4 semanas de recurrentes con el backend de producción:

```bash
go run ./cmd/migrate-recurring-bookings                       # dry-run
APPLY=true API_BASE_URL=https://<backend-produccion> RECURRING_JOB_TOKEN=<token> \
  go run ./cmd/migrate-recurring-bookings
```

   Anotar el instante `inicio=...` que imprime (sirve para el rollback con `SINCE`). Debe terminar con `OK`: segunda corrida sin crear nada, sin duplicados y cada serie con sus fechas.
3. Si `BOOKING_REDIS_AUTHORITY=true`: ejecutar `reconcile-occupancy`.
4. Levantar la app y habilitar el workflow diario de GitHub (06:00 UTC). Desde aquí agrega solo la ocurrencia siguiente de cada serie.

## 5. Verificación

Base de datos (`mongosh`):

```js
db.bookings.countDocuments({scheduled_at: {$exists: false}})   // 0
db.bookings.countDocuments({payment_method: "fintoc"})          // 0
db.sport_centers.countDocuments({fintoc: {$exists: true}})      // 0
db.bookings.aggregate([{$match: {status: "confirmed"}},
  {$group: {_id: {c: "$court_id", d: "$local_date", h: "$hour", m: "$minutes"}, n: {$sum: 1}}},
  {$match: {n: {$gt: 1}}}]).toArray().length                    // 0
db.sport_centers.find({}, {slug: 1, courts_count: 1, "commercial.payment_mode": 1, location: 1, timezone: 1})
```

Esperado: Orellana `PAY_ONLINE` con comisión 0% (`source=CENTER`), Catolica `PAY_AT_CENTER` con suscripción `LEGACY` en monto 0, `location` y `timezone` presentes, credenciales de Mercado Pago iguales a las del snapshot.

API y flujos:
- [ ] `GET /api/sport-centers?lat=..&lng=..` devuelve los centros ordenados por distancia.
- [ ] `GET /api/sport-centers/slug/catolica/facilities` lista los quinchos; la disponibilidad de un quincho es de día completo.
- [ ] Disponibilidad de Catolica Cancha 1 un miércoles: 13 franjas.
- [ ] Reserva con pago online en Orellana (Mercado Pago) y reserva presencial en Catolica.
- [ ] Calendario del administrador (web y apps móviles de administración con la versión instalada).
- [ ] Los logs `[Migrate]` están completos y ninguno terminó sin `FIN` salvo intentos fallidos conocidos.

## 6. Rollback

| Paso | Cómo |
|---|---|
| Reservas de Fintoc | `ROLLBACK=true APPLY=true go run ./cmd/remove-fintoc-bookings` |
| Duplicados | `ROLLBACK=true APPLY=true go run ./cmd/migrate-resolve-duplicates` |
| Quinchos | `ROLLBACK=true APPLY=true go run ./cmd/migrate-quincho-day` |
| Modelo comercial | `ROLLBACK=true APPLY=true go run ./cmd/migrate-commercial-model` |
| Recurrentes | `ROLLBACK=true APPLY=true SINCE=<inicio> go run ./cmd/migrate-recurring-bookings` |
| Credenciales de Fintoc | Solo desde el `mongodump` previo (no hay copia en la base) |
| Todo | Restaurar el snapshot de Atlas y volver al backend anterior (lo más seguro) |

`spaces`, `schedule_rules` y los campos temporales de `bookings` no tienen rollback propio: `courts` se conserva intacta hasta la limpieza final, y el backend anterior sigue funcionando sobre ella.

## 7. Después de la migración

- [ ] **Revocar las llaves de Fintoc** en su panel (estuvieron en texto plano).
- [ ] Actualizar la descripción de los quinchos de Catolica ("Horario 19 a 22 hrs") con su administrador.
- [ ] Eliminar los respaldos temporales con secretos (dumps de `sport_centers`).
- [ ] Más adelante, borrar `courts` (y `facilities` si existiera) como paso aparte, sin volver a correr `migrate-spaces`.
- [ ] Activar `COMMERCIAL_MODEL_ENFORCED=true` solo cuando la Fase 5 (frontend de configuración comercial) esté lista. Con el flag encendido Orellana pasa de pagar 7% efectivo a 0%, y los centros nuevos nacen sin publicar.
- [ ] Guardar los archivos de `logs/migrations/` de esta ejecución junto con el acta de la ventana.
