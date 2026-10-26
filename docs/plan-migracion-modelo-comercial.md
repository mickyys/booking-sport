# Plan de migración al modelo comercial (SOLO RESERVAS / PAGO ANTICIPADO)

Diseño de referencia: `docs/refactor-modelo-comercial.md` (raíz del workspace).
Informe de análisis: `docs/analisis-migracion-modelo-comercial.md` (raíz del workspace).

Base del análisis: copia local `sport_booking_prod` (contenedor `booking_sport_db`), restaurada desde `sport_booking` en Atlas el 2026-10-25. Los conteos coinciden con producción en las 7 colecciones.

## 1. Decisiones tomadas

| # | Tema | Decisión |
|---|---|---|
| 1 | Club Union Catolica | `PAY_AT_CENTER` |
| 2 | Comisión | Centros migrados: 0%. Centros nuevos: 7% por defecto |
| 3 | Suscripción mensual | Monto 0 |
| 4 | Fintoc | Se retira. Sus reservas no se migran y se eliminan (con respaldo en `migration_backups`) |
| 5 | Cobro de suscripción | Mercado Pago |
| 6 | Centros existentes | Siguen publicados; no se les exige suscripción en el primer despliegue |

## 2. Valores actuales (producción)

### 2.1 Inventario

| Colección | Documentos | Observaciones |
|---|---|---|
| sport_centers | 2 | sin `status`, `onboarding_*`, `timezone`, `tenant_id`, `payment_mode` ni `marketplace_fee_percent` |
| courts | 6 | no existen `spaces` ni `schedule_rules` |
| bookings | 1375 | 1159 sin `scheduled_at` ni `timezone`; ninguna con `pricing` |
| recurring_reservations | 85 | 50 activas (21 Orellana, 29 Catolica) |
| user_devices | 13 | |
| users | 0 | |
| slot_holds | 0 | |

### 2.2 Centros

| Centro | Id | Mercado Pago | Fintoc | Reservas |
|---|---|---|---|---|
| Orellana Centro Deportivo | `69b601e1989ae3a65d3e01b0` | token presente | sí | 1146 (284 MP confirmadas; incluye 16 de Fintoc que se eliminan, ver sección 4) |
| Club Union Catolica | `69e054444604771de4b6f745` | token presente, sin cobros | no | 229 (solo `internal` y `presential`) |

Cobros online confirmados en Orellana:
- Mercado Pago: 284 reservas, $8.392.000 (2026-05-01 a 2026-10-26).
- Fintoc: 11 confirmadas ($255.000) y 16 en total (2 canceladas, 3 expiradas), de 2026-03-20 a 2026-04-02. No forman parte de la migración y se eliminan (`remove-fintoc-bookings`).

### 2.3 Comisión actual
- `defaultMarketplaceFeePercent = 7` en `internal/app/mp_payment_usecase.go`.
- Un `marketplace_fee_percent <= 0` cae a ese 7%.
- Orellana no tiene fee propio: **paga 7% efectivo hoy**.
- Ninguna reserva histórica guarda comisión (`pricing` ausente en las 1375), por lo que no se hace backfill.

### 2.4 Riesgos
1. Producción está en un esquema anterior: son obligatorias las migraciones previas (sección 4, fase 1).
2. Un fee de 0 hoy significa "usar el default". El modelo nuevo debe distinguir "sin valor" de "0% explícito".
3. Las credenciales de Mercado Pago y Fintoc están en texto plano en `sport_centers`.
4. La contraseña de Atlas se compartió por chat: rotarla y usar un usuario de solo lectura para dumps.

## 3. Resultado esperado

### 3.1 Centros

| Centro | Modalidad | Comisión | Mercado Pago | Suscripción | Estado |
|---|---|---|---|---|---|
| Orellana | `PAY_ONLINE` | 0% (`source=CENTER`, `value=0`) | `CONNECTED` | no aplica | `PUBLISHED` |
| Club Union Catolica | `PAY_AT_CENTER` | no aplica | credenciales conservadas sin uso | monto 0, activa (existente) | `PUBLISHED` |
| Centros nuevos | a elegir en onboarding | 7% (`source=DEFAULT`) si `PAY_ONLINE` | según modalidad | según modalidad | según onboarding |

Nota: Orellana paga 7% hasta activar el flag `COMMERCIAL_MODEL_ENFORCED` y 0% después. Es un cambio de ingresos aceptado.

### 3.2 Datos que no deben cambiar (checksum antes y después)

| Colección | Esperado |
|---|---|
| bookings | 1375 sin modificaciones |
| recurring_reservations | 85 sin modificaciones |
| courts / spaces | 6 sin modificaciones de horarios ni precios |
| credenciales MP y Fintoc | sin modificaciones |
| `payment_mode` y `status` de centros | sin modificaciones salvo Catolica (`payment_mode=pay_at_center`) |

Segunda ejecución de la migración: 0 cambios.

### 3.3 Documento `commercial` esperado

Orellana:
```json
{
  "payment_mode": "PAY_ONLINE",
  "online_payment": {
    "commission": { "type": "PERCENTAGE", "value": 0, "source": "CENTER" },
    "payment_provider": { "provider": "MERCADO_PAGO", "status": "CONNECTED" }
  },
  "migration": "commercial-model-v1",
  "review": false
}
```

Catolica:
```json
{
  "payment_mode": "PAY_AT_CENTER",
  "subscription": { "status": "ACTIVE", "plan": "LEGACY", "amount": 0 },
  "migration": "commercial-model-v1",
  "review": false,
  "reason": "decisión del administrador"
}
```

## 3.4 Registro (logs) de la migración

Todo comando del proceso (`migrate-*`, `remove-*` y `audit-*`) deja un archivo de log con el tag `[Migrate]`, además de mostrarlo en consola:

- Ubicación: `logs/migrations/` (cambiable con `MIGRATE_LOG_DIR`), un archivo por ejecución: `migrate-<comando>-<AAAAMMDD-HHMMSS>.log` (hora UTC). La carpeta está ignorada por git (`logs/`).
- Cada línea empieza con `[Migrate]`, incluida la salida de `fmt.Printf` de las auditorías.
- Al inicio registra `INICIO` (comando, pid, host), la ruta del archivo y la configuración (`MONGO_URI` con usuario y contraseña ocultos y solo variables permitidas como `APPLY`, `DRY_RUN`, `ROLLBACK`, `CENTER_ID`, `SPACE_IDS`). Nunca registra tokens ni secretos.
- Al terminar bien registra `FIN` con la duración. **Un archivo sin línea `FIN` es una ejecución fallida**: su última línea es el error.
- Implementación: `pkg/migratelog` (`defer migratelog.Start("<comando>")()` al inicio de `main`). Todo comando nuevo del proceso debe usarla.
- Conservar los archivos de cada ejecución en producción como evidencia de lo ejecutado (dry-run, apply y rollback).

## 4. Fases

### Fase 0 — Preparación (hecha)
- Copia local `sport_booking_prod` y verificación de conteos.
- `cmd/migrate-commercial-model` con dry-run, apply, idempotencia y rollback, probada.
- Ajustado: comisión 0% explícito para migrados, `OVERRIDES` (Catolica), suscripción LEGACY monto 0, Fintoc `RETIRED`.

### Fase 1 — Migraciones previas (copia local, luego producción)
Siempre con dry-run primero.

```bash
export MONGO_URI=mongodb://localhost:27017 MONGO_DB=sport_booking_prod
make audit-indexes
make audit-bookings
go run ./cmd/remove-fintoc-bookings && APPLY=true go run ./cmd/remove-fintoc-bookings
go run ./cmd/migrate-spaces
go run ./cmd/migrate-geo
DRY_RUN=true go run ./cmd/migrate-schedule-rules && go run ./cmd/migrate-schedule-rules
go run ./cmd/migrate-quincho-day && APPLY=true go run ./cmd/migrate-quincho-day
# Último paso (ver "Generación inicial de recurrentes"):
go run ./cmd/migrate-recurring-bookings
go run ./cmd/migrate-sport-center-timezone && APPLY=true go run ./cmd/migrate-sport-center-timezone
go run ./cmd/migrate-booking-schedule-fields && APPLY=true go run ./cmd/migrate-booking-schedule-fields
```

Verificación esperada: `spaces` = 6, 0 reservas sin `scheduled_at`, sin franjas de duración cero, 0 centros sin timezone.

**`migrate-geo`.** Agrega `location` (GeoJSON) a los centros y crea el índice 2dsphere. Sin él, `GET /api/sport-centers?lat=..&lng=..` devuelve vacío.

**Horarios específicos de un día (`migrate-schedule-rules`).** En el modelo legacy un slot con `day_of_week` se suma a los generales; en las reglas, la regla más específica reemplaza a la general. La conversión se corrigió (`BuildRulesFromLegacyCourtSchedule`): la regla de un día incluye también los slots generales que no se solapan con los suyos. Caso real: Catolica Cancha 1 tiene un slot solo para el miércoles (17:30) y, sin la corrección, el miércoles quedaba con 1 franja en vez de 13 (y 3 recurrentes no podían generarse). Para regenerar solo ese espacio sobre una base ya migrada:

```bash
SPACE_IDS=69e3096507fc362414077f27 FORCE=true go run ./cmd/migrate-schedule-rules
```

No usar `FORCE=true` sin `SPACE_IDS`: rehace también las reglas de los quinchos. En una base nueva basta con la ejecución normal de la Fase 1 (ya usa la conversión corregida). Resultado esperado: miércoles con 13 franjas (10:00-16:00, 17:30, 19:00-23:00; las de 17:00 y 18:00 se ocultan por solaparse con 17:30, igual que en el modelo legacy).

**Generador de recurrentes (prueba en copia local).** Con las reglas corregidas, `POST /api/internal/jobs/recurring-generate` crea 196 reservas (49 series × 4, del 26-10 al 21-11) sin omitidas, sin fallos ni duplicados confirmados, y una segunda corrida no crea nada. El job diario lo dispara GitHub Actions (`booking-sport-batch`, 06:00 UTC, secreto `RECURRING_JOB_TOKEN`, variable `API_BASE_URL`): revisar que esté configurado antes de activarlo en producción, porque la primera ejecución generará de golpe esas reservas.

**Reservas de Fintoc (`remove-fintoc-bookings`).** Se ejecuta ANTES de las demás migraciones de reservas (el backfill y la generación no deben considerarlas). Elimina las reservas con `payment_method=fintoc` o `fintoc_payment_id`/`fintoc_payment_intent_id`; en Orellana son 16 (11 confirmadas por $255.000, 2 canceladas y 3 expiradas, marzo-abril 2026, sin fechas futuras). Cada documento se copia antes a `migration_backups` (tag `fintoc-bookings-v1`).
- Dry-run por defecto; `APPLY=true go run ./cmd/remove-fintoc-bookings` aplica; `ROLLBACK=true APPLY=true` restaura desde `migration_backups`. Idempotente.
- Aborta si hay reservas Fintoc confirmadas con fecha futura (`FORCE_FUTURE=true` lo permite).
- No borra las credenciales Fintoc de `sport_centers` ni cambia nada más.
- Orden recomendado en producción: respaldo de `bookings`, `remove-fintoc-bookings`, y luego el resto de la Fase 1.

**Generación inicial de recurrentes (`migrate-recurring-bookings`).** Último paso de la Fase 1. Genera las primeras 4 semanas de todas las recurrentes activas con el mismo generador que el job diario, vinculando cada reserva a su serie con `recurring_id`. Las reservas de la migración anterior usan `series_id` (otro mecanismo) y no se tocan; si una ocurrencia choca con una reserva existente, el generador la omite y queda en `generation_issues`.

- Requisitos: Fase 1 completa (reglas de horario corregidas, quinchos, `local_date`), duplicados resueltos y un backend apuntando a la misma base con `RECURRING_JOB_TOKEN`.
- Dry-run (por defecto): lista por recurrente las fechas a generar y los conflictos conocidos con reservas confirmadas.
- Apply: `APPLY=true API_BASE_URL=<backend> RECURRING_JOB_TOKEN=<token> go run ./cmd/migrate-recurring-bookings`. Ejecuta dos corridas y verifica que la segunda no cree nada, que lo creado coincida con las reservas `recurring_id` nuevas, que no haya horarios con más de una reserva confirmada y que cada serie tenga todas sus fechas; sale con error si algo falla.
- Rollback: `ROLLBACK=true APPLY=true SINCE=<instante de inicio que imprime el apply> go run ./cmd/migrate-recurring-bookings` borra las reservas `recurring_id` generadas desde ese instante. Respaldar `bookings` y `recurring_reservations` antes.
- Resultado en `sport_booking`: 196 reservas (49 series × 4, del 26-10 al 21-11; Catolica 112 y Orellana 84), sin duplicados; segunda corrida con 0 creadas.
- Después de este paso, el job diario de GitHub Actions solo agrega la ocurrencia siguiente de cada serie.

**`migrate-quincho-day` (quinchos de Catolica).** En producción los 3 quinchos de Catolica ("Quincho 1-3") están en `courts` con una franja de 19:00 a $15.000, así que `migrate-spaces` los deja como `cancha`. Este comando los convierte a `category=quincho` con reserva de día completo:
- `spaces`: `category=quincho`, `type=quincho`, `price_all_day=15000`; se conserva el `_id`.
- `schedule_rules`: una regla de día completo (00:00-24:00) por espacio.
- `bookings`: cada reserva pasa a `hour=0`, `duration_minutes=1440`, `scheduled_at`/`ends_at` en el día local (primer instante válido del día y del siguiente, DST-aware) y `space_category=quincho`. Precio, estado, código y cliente no cambian.
- `courts_count` se recalcula (Catolica queda en 1).
- Respaldo del estado original en `migration_backups` (tag `quincho-day-v1`); `ROLLBACK=true APPLY=true` lo revierte. Idempotente.

Ejecutarlo siempre DESPUÉS de `migrate-spaces`, `migrate-schedule-rules` y `migrate-booking-schedule-fields` (necesita `local_date`) y antes de cualquier nueva ejecución de `migrate-spaces`, que volvería a marcar esos espacios como `cancha`. Falla si hay dos reservas activas el mismo día en un quincho (resolver duplicados antes). La `description` del espacio ("Horario 19 a 22 hrs") es texto del administrador y no se modifica: revisarla con el centro.

**Bloqueo conocido en `migrate-booking-schedule-fields` (reservas duplicadas).** El backfill normaliza `date` y choca con los índices únicos de reservas confirmadas si hay dos en el mismo horario (el error es `E11000 ... idx_bookings_unique_confirmed_slot_v2`). Hay 5 reservas confirmadas en 2 horarios duplicados en la cancha `69c095fd28c02626758a5b62` de Orellana. Antes del apply hay que cancelar las duplicadas, con respaldo previo de `bookings`:

| Horario | Conservar | Cancelar |
|---|---|---|
| 2026-05-13 21:00 | `916b3524` | `633c7e11`, `3a46b3e1` |
| 2026-05-18 22:00 | `30d5c5c9` | `b5427ce4` |

```js
db.bookings.updateMany(
  { booking_code: { $in: ["633c7e11", "3a46b3e1", "b5427ce4"] }, status: "confirmed" },
  { $set: { status: "cancelled", cancelled_by: "migration:phase1",
    cancellation_reason: "Reserva duplicada del mismo horario detectada en la migración de esquema (fase 1)",
    updated_at: new Date() } })
```

Si en producción aparecen más duplicados, el apply fallará con el mismo error: revisar primero con un agrupamiento por (`court_id`, día civil, `hora`, `minutes`) sobre las reservas confirmadas.

**Verificación de solapes por duración (tras el backfill).** `audit-bookings` debe reportar 0 pares solapados. En la copia local: las 1375 reservas duran 60 minutos y empiezan a hora en punto, sin solapes entre las 955 activas.

**Recurrentes duplicadas.** Dos recurrentes activas de "Alonso" (cancha `69e3096507fc362414077f27`, jueves 19:00, $25.000). Se conservó `69e6d7041313560aaf6f8ef7` (la gestionada por el administrador, con fechas canceladas) y se canceló `69e6ca4e5526854c22ccd068` (`status=cancelled`, `cancelled_by=migration:phase1`). Repetir en producción tras confirmar con el administrador de Catolica.

### Fase 2 — Campos nuevos sin cambiar comportamiento
- `commercial` opcional en `domain.SportCenter`; si falta, el comportamiento es el actual.
- Estados: DRAFT, CONFIGURING, PENDING_SUBSCRIPTION, PENDING_PAYMENT_SETUP, READY, PUBLISHED, SUSPENDED. Un centro sin `status` y con espacios se trata como PUBLISHED.
- Comisión con `value` como puntero o `source` explícito, para distinguir 0% de "sin valor".
- `CommissionSnapshot` (`rate`, `amount`) también en reservas de canchas.
- Todo detrás de `COMMERCIAL_MODEL_ENFORCED=false`.

### Fase 3 — Migración de centros existentes
```bash
DEFAULT_COMMISSION=0 OVERRIDES=69e054444604771de4b6f745=PAY_AT_CENTER go run ./cmd/migrate-commercial-model
APPLY=true DEFAULT_COMMISSION=0 OVERRIDES=69e054444604771de4b6f745=PAY_AT_CENTER go run ./cmd/migrate-commercial-model
```
- Orellana: `PAY_ONLINE`, comisión 0% explícito, Mercado Pago `CONNECTED`.
- Catolica: `PAY_AT_CENTER`, suscripción monto 0, se fija `payment_mode=pay_at_center`, credenciales conservadas.
- Fintoc: proveedor marcado `RETIRED`; las credenciales de `sport_centers` no se tocan. Las reservas de Fintoc se eliminan antes de migrar con `remove-fintoc-bookings`.
- Respaldo previo de `sport_centers` antes del apply.

### Fase 4 — Backend
- `CanPublishSportCenter(center)`: validaciones generales, más suscripción `ACTIVE` (`PAY_AT_CENTER`) o Mercado Pago `CONNECTED` (`PAY_ONLINE`), siempre en backend.
- Comisión resuelta en cada cobro y guardada (`rate`, `amount`) en la transacción; nunca se recalcula el histórico. Debe leerla el modelo nuevo en `booking_usecase.go`, `mp_reconcile_usecase.go`, `facility_booking_usecase.go` y `mp_payment_usecase.go`.
- Default global 7% solo para centros nuevos.
- Fintoc ya no existe en el checkout ni en la configuración; sus reservas se eliminan (ver `remove-fintoc-bookings`).
- Suscripción con Mercado Pago (preapproval) y webhook que actualiza `subscription.status`.
- Endpoints: obtener y configurar `commercial`, cambiar modalidad, suscripción, estado de Mercado Pago, comisión por centro (superadmin), publicar y validar publicación.
- Cambio de modalidad: nunca borra espacios, horarios, reservas ni comisiones históricas.
- Con el flag apagado, ningún centro existente se bloquea.

### Fase 5 — Panel y frontend
- Superadmin: "Configuración comercial" (modalidad, suscripción, Mercado Pago, comisión efectiva y origen).
- Onboarding: selección de modalidad, pasos según modalidad, progreso y botón de publicar.
- App móvil admin: solo mostrar estado, sin romper versiones instaladas.

### Fase 6 — Pruebas
- Unitarias: reglas de publicación; comisión 0% por centro, 7% por defecto en centros nuevos; cambio de modalidad.
- Integración: publicación, suscripción, Mercado Pago, comisión histórica inmutable, migración (idempotencia y rollback).
- Checksums antes y después de la migración.

### Fase 7 — QA y producción
1. Desplegar backend con el flag apagado.
2. Respaldo de producción (snapshot de Atlas o `mongodump`).
3. Migraciones previas (fase 1) con dry-run.
4. `migrate-commercial-model` con dry-run, revisión y apply.
5. Verificar que Orellana y Catolica siguen publicados y reservando, y que los pagos funcionan.
6. Activar el onboarding nuevo para centros nuevos.
7. Activar reglas comerciales de forma progresiva (primero `PAY_ONLINE`, luego suscripciones).

## 5. Verificación

```bash
docker exec booking_sport_db mongosh --quiet sport_booking_prod --eval '
  printjson(db.sport_centers.find({}, {name:1, "commercial.payment_mode":1,
    "commercial.online_payment.commission":1, "commercial.review":1}).toArray());
  print("bookings:", db.bookings.countDocuments({}));
  print("recurring:", db.recurring_reservations.countDocuments({}));
  print("sin commercial:", db.sport_centers.countDocuments({commercial:{$exists:false}}));
'
```

Esperado: bookings 1375, recurring 85, sin `commercial` 0, 0 centros con `review=true`.

## 6. Rollback
- `ROLLBACK=true APPLY=true go run ./cmd/migrate-commercial-model` quita `commercial` solo de los centros migrados por este comando.
- Flag `COMMERCIAL_MODEL_ENFORCED=false` restaura el comportamiento anterior.
- Las migraciones previas no borran `courts` ni `spaces.schedule` hasta la limpieza final.

## 7. Criterios de aceptación
- Los del punto 13 de `docs/refactor-modelo-comercial.md`.
- Reservas, recurrentes, espacios, horarios y precios idénticos antes y después.
- Orellana sigue cobrando por Mercado Pago sin interrupción.
- Catolica queda `PAY_AT_CENTER` y publicada.
- Fintoc no es opción de pago nueva y sus reservas no existen en `bookings`.
- Segunda ejecución de la migración: 0 cambios.

## 8. Estado de implementación (copia local `sport_booking`)

- Fase 3 aplicada en `sport_booking` (respaldo previo en `sport_booking_local` y archivo de `sport_centers`). Checksums de bookings, recurring_reservations, courts y user_devices idénticos; segunda ejecución sin cambios; rollback y re-apply probados.
- Fase 4 implementada detrás de `COMMERCIAL_MODEL_ENFORCED` (por defecto `false`):
  - `domain/commercial.go`: modelo, `CanPublishSportCenter`, comisión efectiva (0% explícito).
  - `app/commercial_usecase.go`: modalidad, comisión, publicación, suscripción Mercado Pago y webhook.
  - Rutas admin: `GET /api/admin/commercial`, `PUT .../mode`, `GET .../publish-check`, `POST .../publish`, `GET|POST .../subscription`.
  - Rutas superadmin: `GET /api/superadmin/sport-centers/:id/commercial`, `PUT .../commercial/mode`, `PUT .../commercial/commission` (`{"value": 0..100 | null}`), `POST .../commercial/publish`.
  - Webhook público: `POST /api/subscriptions/mercadopago/webhook`.
  - Variables: `COMMERCIAL_MODEL_ENFORCED`, `SUBSCRIPTION_MONTHLY_AMOUNT` (CLP, sin él no se crean suscripciones), `SUBSCRIPTION_BACK_URL`.
- Fase 1 aplicada en `sport_booking`: `spaces` = 6, `schedule_rules` = 7, 0 centros sin timezone y 1375 reservas con `scheduled_at`. Se canceló `633c7e11`, `3a46b3e1` y `b5427ce4` (duplicadas); respaldo previo de `bookings` fuera del repo. `audit-bookings` sin hallazgos. También se canceló la recurrente duplicada `69e6ca4e5526854c22ccd068` (queda activa `69e6d7041313560aaf6f8ef7`): 49 activas, 25 finalizadas y 11 canceladas en `recurring_reservations`, sin duplicadas activas.
- `migrate-geo` y `migrate-quincho-day` aplicados en `sport_booking`: Catolica queda con 1 cancha y 3 quinchos por día completo (`courts_count`=1). Verificado por API: `/facilities` lista los 3 quinchos, la disponibilidad es de día completo y 2026-08-09 aparece reservado.
- `migrate-recurring-bookings` aplicado en `sport_booking`: 196 reservas con `recurring_id` (26-10 al 21-11), 0 omitidas, 0 fallos, segunda corrida sin cambios.
- Fintoc: 16 reservas eliminadas de `sport_booking` (respaldo en `migration_backups`); los campos `fintoc_*` se quitaron de `Booking` y del request de `SyncConfirmedPayment`. Las credenciales Fintoc de `sport_centers` siguen sin tocar.
- Pendiente: la Fase 5 (frontend).
