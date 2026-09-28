# Migraciones de datos — ReservaloYA (booking-sport)

Runbook de migraciones para **staging y producción**. Todas se ejecutan desde la raíz del módulo Go (`booking-sport/`) y son **idempotentes** (re-ejecutarlas no duplica datos).

## Requisitos

- Tener la URI de MongoDB. Local: `mongodb://localhost:27017`. En staging/producción usar la URI real de Atlas.
- Base por defecto: `sport_booking`. Se puede cambiar con `MONGO_DB`.
- Compilar antes si se quiere un binario (opcional; los ejemplos usan `go run`):

```bash
go build ./...
go vet ./...
```

> **Importante:** los endpoints de la nueva configuración (`/admin/spaces/:id/schedule`) requieren el backend desplegado con el código nuevo. Ejecuta la migración **después** de desplegar el backend nuevo (o al menos con la colección `schedule_rules` existente).

---

## 1. Migración `courts` y `facilities` → `spaces`

Une las colecciones legacy `courts` (canchas) y `facilities` (quinchos/sedes/piscinas) en una única colección `spaces` con campo `category`. Preserva los `_id`, por lo que `bookings.court_id`, `recurring_reservations.court_id`, `slot_holds.court_id` y `facility_inquiries.facility_id` siguen apuntando a IDs válidos.

Comando: `cmd/migrate-spaces`

```bash
# Previsualización no es necesaria: es upsert por _id, sin destruir nada por defecto.
MONGO_URI=mongodb://localhost:27017 go run ./cmd/migrate-spaces

# Solo cuando se validó en staging: eliminar las colecciones originales
DROP_ORIGINAL=true MONGO_URI=mongodb://localhost:27017 go run ./cmd/migrate-spaces
```

Variables:

| Variable | Descripción | Default |
|---|---|---|
| `MONGO_URI` | URI de MongoDB | `mongodb://localhost:27017` |
| `MONGO_DB` | Nombre de la base | `sport_booking` |
| `DROP_ORIGINAL` | Si `true`, elimina `courts` y `facilities` al terminar | `false` |

Verificación:

```bash
docker exec booking_sport_db mongosh --quiet sport_booking --eval '
  print("spaces:", db.spaces.countDocuments({}));
  print("canchas:", db.spaces.countDocuments({category:"cancha"}));
  print("courts (legacy):", db.courts.countDocuments({}));
'
```

> En un entorno ya migrado, `courts` puede estar vacía o no existir. Si `DROP_ORIGINAL=true` no se usó, se conservan como respaldo.

---

## 2. Migración `spaces.schedule` (legacy) → `schedule_rules`

Convierte el horario embebido `spaces.schedule` (slots por hora del modelo anterior) a la colección `schedule_rules` (reglas semanales con franjas por rango `start_time`/`end_time`).

Reglas de conversión:
- Cada slot de 1h → una franja de 60 min con su precio y reglas de pago (`payment_required`, `payment_optional`, `partial_payment_enabled`).
- Slots con `day_of_week == null` → una regla para los 7 días.
- Slots con `day_of_week` específico → una regla para ese día (ej. el lunes a las 15:30 → regla `days_of_week=[1]` con franja `15:30–16:30`).
- Slots con `status == "closed"` se omiten.
- Slots que cruzan pasada la medianoche (>24:00) se omiten; los que terminan a las 24:00 quedan como franja `xx:00–24:00`.

Comando: `cmd/migrate-schedule-rules`

```bash
# 1) Previsualización (NO escribe nada)
DRY_RUN=true MONGO_URI=mongodb://localhost:27017 go run ./cmd/migrate-schedule-rules

# 2) Ejecutar (idempotente: salta reglas ya existentes)
MONGO_URI=mongodb://localhost:27017 go run ./cmd/migrate-schedule-rules

# 3) Solo si hay que regenerar (sobrescribe reglas existentes derivadas del legacy)
FORCE=true MONGO_URI=mongodb://localhost:27017 go run ./cmd/migrate-schedule-rules
```

Variables:

| Variable | Descripción | Default |
|---|---|---|
| `MONGO_URI` | URI de MongoDB | `mongodb://localhost:27017` |
| `MONGO_DB` | Nombre de la base | `sport_booking` |
| `DRY_RUN` | Si `true`, solo reporta qué haría (no escribe) | `false` |
| `FORCE` | Si `true`, reemplaza reglas existentes del mismo espacio y días | `false` |

### Ejemplo (datos locales)

Para el espacio `69dfe61fe90ccbf767e08f51` (cancha con horario por día, incluye lunes 15:30):

```bash
DRY_RUN=true MONGO_DB=sport_booking MONGO_URI=mongodb://localhost:27017 go run ./cmd/migrate-schedule-rules
```

Verificación:

```bash
docker exec booking_sport_db mongosh --quiet sport_booking --eval '
  const rules = db.schedule_rules.find({space_id: ObjectId("69dfe61fe90ccbf767e08f51")}).toArray();
  print("reglas del espacio:", rules.length);
  for (const r of rules) {
    print("  days=" + JSON.stringify(r.days_of_week) + " franjas=" + r.slots.length);
    for (const s of r.slots) print("    " + s.start_time + "-" + s.end_time + " $" + (s.price||0));
  }
'
```

> La ruta web `admin/spaces/69dfe61fe90ccbf767e08f51/schedule` es la **UI** para editar reglas/excepciones/bloqueos de un espacio. La migración es **global** (todas las canchas) y se corre una sola vez; después la UI escribe/actualiza las reglas directamente.

### Sanity check global (sin franjas inválidas)

```bash
docker exec booking_sport_db mongosh --quiet sport_booking --eval '
  const all = db.schedule_rules.find({}).toArray();
  let total = 0, zero = 0;
  for (const r of all) for (const s of r.slots) {
    total++;
    if (s.start_time === s.end_time) zero++;
  }
  print("reglas:", all.length, "| franjas:", total, "| duracion cero:", zero);
'
```

---

## Orden recomendado (staging y producción)

1. **Desplegar backend nuevo** (con `schedule_rules`/`schedule_exceptions`/`blocks` e índices — se crean automáticamente en el arranque).
2. Ejecutar `migrate-spaces` si aún no se unificó a `spaces` (idempotente).
3. Ejecutar `migrate-schedule-rules` con `DRY_RUN=true` → revisar salida.
4. Ejecutar `migrate-schedule-rules` (sin flags) → `FORCE=true` solo si es necesario regenerar.
5. Verificar con los sanity checks de arriba.
6. Probar en staging: agenda pública, checkout/MercadoPago, reservas recurrentes y la pantalla `admin/spaces/:id/schedule`.

---

## Notas de compatibilidad

- La disponibilidad y las reservas siguen funcionando con el modelo legacy (`spaces.schedule`) para espacios **sin** `schedule_rules`. El nuevo motor (reglas → excepciones → bloqueos → bookings) se activa automáticamente cuando el espacio tiene reglas.
- `migrate-spaces` y `migrate-schedule-rules` NO borran el campo `spaces.schedule`; queda como respaldo hasta la limpieza final (fase 10).
