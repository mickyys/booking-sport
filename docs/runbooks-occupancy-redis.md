# Runbooks — coordinador de ocupación (Redis / Upstash)

Redis decide si un intervalo de un espacio está libre. MongoDB sigue siendo la
fuente de verdad de las reservas. Con `BOOKING_REDIS_AUTHORITY=true` **ninguna
escritura de reservas se degrada a MongoDB si Redis falla**: se rechaza.

Variables: `REDIS_URL`, `REDIS_KEY_PREFIX`, `BOOKING_REDIS_AUTHORITY`,
`BOOKING_OCCUPANCY_MODE`,
`BOOKING_MAX_DURATION`, `BOOKING_RECONCILE_INTERVAL`. El token de Upstash vive
sólo en el gestor de secretos; en documentación se usa `<TOKEN>`.

Comando de apoyo (siempre DRY_RUN salvo `APPLY=true`):

```bash
MODE=import|reconcile [APPLY=true] [REBUILD=true] [CENTER_ID=...] [LIMIT=...] \
MONGO_URI=... MONGO_DB=... REDIS_URL=... REDIS_KEY_PREFIX=... \
go run ./cmd/reconcile-occupancy
```

## Comportamiento verificado durante una caída

Simulado con `make test-redis-outage` (detiene el contenedor Redis a mitad de la
prueba) y con tests de `internal/app/occupancy_guard_test.go`.

| Operación | Redis caído |
|---|---|
| Crear reserva (interna, legacy, pago, recurrencia) | **503**, no se escribe en MongoDB |
| Confirmar pago (webhook / sincronización) | Falla; MercadoPago reintenta el webhook |
| Cancelar | La cancelación en MongoDB se completa; la liberación queda para el reconciliador |
| Reprogramar | Rechazada (503) |
| Lecturas (historial, agenda, detalle) | Funcionan: no usan Redis |
| Fallo de conexión | Falla en < 1 s con el timeout configurado, no se cuelga |

Tras volver Redis, el cliente reconecta solo (verificado). **Un Redis local sin
persistencia pierde los datos al reiniciarse**; Upstash persiste, pero su
réplica es asíncrona (ver RPO en el plan §1.2).

## 1. Caída o degradación de Upstash

Síntomas: 503 en reservas, log `occupancy_coordinator_unavailable`, `Health`
falla.

1. Confirmar en el panel de Upstash (estado, throttling, budget, memoria).
2. **No** poner `BOOKING_REDIS_AUTHORITY=false` como atajo mientras haya
   tráfico: reabriría el riesgo de doble reserva. Aceptar el 503 durante la
   caída es el comportamiento correcto.
3. Al volver el servicio, lanzar `MODE=reconcile` (dry-run) y revisar:
   `confirmed_missing_in_redis` > 0 implica pérdida de datos → runbook 3.
4. `MODE=reconcile APPLY=true` repara `committing` y libera ocupaciones muertas.

## 2. Throttling / budget / memoria

Síntomas: errores de límite en logs, escrituras rechazadas por Upstash.

1. Revisar plan y cuotas (decisión pendiente P4 del plan).
2. Verificar que la eviction siga **deshabilitada**: una clave evictada sería
   un horario libre falso.
3. Si la memoria crece, ejecutar `MODE=reconcile APPLY=true`: libera
   ocupaciones de reservas terminadas o canceladas.

## 3. Restauración / reconstrucción tras pérdida de datos en Redis

Usar cuando `reconcile` reporta `confirmed_missing_in_redis` o Redis se reinició
sin persistencia. Ensayado en local (flushall → reconcile → rebuild → reconcile).

1. **Pausar escrituras de reservas** (ventana de mantenimiento) o aceptar que
   los 503 de la caída cubren la ventana.
2. Confirmar que el namespace está vacío: `redis-cli --scan --pattern '<PREFIX>:occ:*' | head`.
   Si no lo está y la pérdida fue parcial, **no** usar `REBUILD` (cada reserva
   chocaría con su ocupación anterior): usar `reconcile`.
3. `MODE=import REBUILD=true` (dry-run). Revisar `conflict`: indica
   solapamientos reales ya existentes en MongoDB; resolverlos antes.
4. `MODE=import REBUILD=true APPLY=true`.
5. `MODE=reconcile` debe devolver sólo `ok`.
6. Reanudar escrituras.

Las reservas **pendientes de pago** cuyo hold ya venció no se importan
(`pending_expired`): el horario queda libre, que es lo correcto.

## 4. Rollback (volver a MongoDB como única barrera)

1. Poner `BOOKING_REDIS_AUTHORITY=false` y reiniciar. Los escritores vuelven al
   comportamiento previo (índices únicos + consultas de solape).
2. Las reservas creadas mientras estuvo apagado **no** estarán en Redis.
3. Para reactivar: runbook de importación (`MODE=import`, luego `reconcile`).
4. Los campos `occupancy_id` y `occupancy_token` en las reservas son inertes con
   el flag apagado.

## 5. Rotación del token de Upstash

1. Crear el nuevo token en Upstash (o rotar la contraseña del endpoint).
2. Actualizar `REDIS_URL` en el gestor de secretos.
3. Despliegue continuo de las instancias: el cliente falla un instante con el
   token viejo (503 breve) y recupera con el nuevo.
4. Verificar `Health` y un `MODE=reconcile` dry-run.
5. Revocar el token anterior. Nunca commitearlo ni pegarlo en tickets.

## 6. Divergencias que reporta el reconciliador

| Hallazgo | Significado | Acción |
|---|---|---|
| `committing_to_booked` | La reserva se confirmó pero el `Commit` falló | Lo repara `APPLY` |
| `released_stale` | Reserva cancelada/expirada/terminada con ocupación viva | Lo repara `APPLY` |
| `link_cleared` | Ocupación ya inexistente; sólo se limpia el vínculo | Automático |
| `confirmed_missing_in_redis` | Reserva activa sin ocupación (Redis perdió datos) | Runbook 3 |
| `confirmed_but_held` | Reserva confirmada con ocupación aún en `held` | Revisar log `occupancy_divergence`; investigar el flujo de pago |
| `inspect_error` | Redis no respondió para esa reserva | Reintentar; ver runbook 1 |

El reconciliador corre cada `BOOKING_RECONCILE_INTERVAL` bajo el lock
`occupancy-reconcile` (`job_locks`), por lo que varias instancias no se pisan.

## 7. Alertas recomendadas (pendiente de instrumentar)

Hoy sólo existen logs estructurales (`occupancy_*`). Pendiente de métricas:
adquisiciones/conflictos, latencia de scripts, `committing` por antigüedad,
divergencias, errores por dependencia, throttling/budget/memoria de Upstash,
`NOSCRIPT` elevado y p99 fuera de umbral.

## 8. Modo sombra y corte por centro (Etapa 7)

Cada centro tiene un modo (`sport_centers.occupancy_authority`) y una
generación (`occupancy_generation`). Los centros sin modo propio usan
`BOOKING_OCCUPANCY_MODE` (por defecto `authority`; usar `off` para activar el
coordinador centro por centro). Las instancias cachean la política ~30 s.

| Modo | Quién decide | Si Redis falla |
|---|---|---|
| `off` | MongoDB | No se usa |
| `shadow` | MongoDB; Redis recibe copia y se comparan decisiones | No afecta a la reserva |
| `authority` | Redis | 503, nunca degrada a MongoDB |

Despliegue previo: `BOOKING_REDIS_AUTHORITY=true` (coordinador disponible),
`BOOKING_OCCUPANCY_MODE=off`. Nada cambia hasta activar un centro.

### Fase sombra

```bash
CENTER_ID=<id> ACTION=status  go run ./cmd/occupancy-cutover
CENTER_ID=<id> ACTION=shadow APPLY=true go run ./cmd/occupancy-cutover
```

Observar en logs: `occupancy_shadow_stats` (cada pasada del reconciliador) y
`shadow_divergence` con `kind`:

- `redis_conflict_mongo_free`: Redis habría rechazado y MongoDB aceptó.
- `mongo_conflict_redis_free`: MongoDB rechazó y Redis habría aceptado.
- `late_payment_redis_conflict`: pago tardío con horario tomado según Redis.

**Criterio de salida:** cero divergencias sin explicar durante N días con carga
real. Las reservas anteriores a la sombra sólo estarán en Redis tras
`MODE=import`; antes de importarlas, esperar `redis_conflict_mongo_free` nulos
pero `mongo_conflict_redis_free` esperables sobre horarios históricos.
`redis_errors` y `stale_generation` no son divergencias pero deben ser ~0.

### Corte a `authority` (por centro)

1. Pausar escrituras del centro (ventana de mantenimiento) y drenar pagos en curso.
2. `MODE=import APPLY=true` (`cmd/reconcile-occupancy`) con `CENTER_ID`.
3. `MODE=reconcile` debe devolver sólo `ok`.
4. `ACTION=authority` sin `APPLY` (dry-run): el comando **se niega** si hay
   reservas activas sin importar o sin campos v2 (`FORCE=true` lo omite; no usar
   salvo emergencia). Las pendientes con hold vencido no cuentan.
5. `ACTION=authority APPLY=true`: publica la generación N+1 en Redis (primero) y
   luego en MongoDB, y fija el modo. Desde ese instante una instancia con la
   generación vieja recibe 503 al adquirir, recarga y reintenta con la nueva.
6. Reabrir escrituras y vigilar 503 y `occupancy_stale_generation` (deben ser
   transitorios, ~30 s).

La guarda de generación se valida en `Acquire` e `Import` (las operaciones que
crean exclusión). Renew, BeginCommit, Commit y Release se validan por token, por
lo que un pago en curso puede terminar a través del corte.

### Rollback de un centro

```bash
CENTER_ID=<id> ACTION=off APPLY=true go run ./cmd/occupancy-cutover
```

Fija `off` y **sube la generación**, de modo que ninguna instancia con una vista
vieja pueda adquirir. Volver a `authority` exige repetir importación y
reconciliación (las reservas hechas en `off` no están en Redis).

## Limitaciones conocidas

- Bloqueos y excepciones de agenda no pasan aún por el coordinador.
- Ocupaciones huérfanas (claves sin reserva) no se detectan todavía.
- Reservas anteriores al flag requieren `MODE=import` antes de activarlo.
