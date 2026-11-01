# Reservas recurrentes semanales

Las reservas recurrentes semanales generan reservas reales en la colección
`bookings` (no solo documentos en `recurring_reservations`). El sistema mantiene
siempre las próximas **4 ocurrencias** de cada serie activa.

## Flujo

```
GitHub Actions (diario 06:00 UTC)
  └─ booking-sport-batch recurring
       └─ POST /api/internal/jobs/recurring-generate   (header X-Job-Token)
            └─ BookingUseCase.GenerateUpcomingRecurringBookings()
                 ├─ lock distribuido en Mongo (job_locks)
                 ├─ FindAllActive()
                 └─ por cada serie: hasta 4 ocurrencias futuras
```

El endpoint es idempotente: si una ocurrencia ya existe no se vuelve a crear.
El lock evita ejecuciones concurrentes y expira automáticamente.

## Reglas de generación

- Se generan exactamente 4 fechas, contadas semana a semana desde la primera
  ocurrencia futura. No se usa una ventana de 28 días.
- Si una fecha tiene conflicto, bloqueo, excepción de cierre, retención de
  horario, pago pendiente u otro bloqueo, **se salta esa semana y se continúa**
  con las siguientes. No se agrega una quinta ocurrencia para compensar.
- Las fechas anuladas manualmente (`cancelled_dates`) tampoco se regeneran.
- La generación es silenciosa: no envía correos ni notificaciones push.
- Las reservas generadas son internas:
  - sin `user_id`, por lo que no aparecen en “Mis reservas”;
  - `payment_method = "internal"`;
  - `recurring_id` apunta a la serie;
  - `series_id` queda vacío para no duplicar la serie en el panel admin.
- Si el precio de la serie es mayor que 0, se usa ese precio fijo. Si es 0, se
  usa el precio vigente de la franja.

## Cancelaciones

- **Anular una fecha** (`POST /admin/recurring/:id/cancel-date`): agrega la fecha
  a `cancelled_dates` y cancela la reserva generada de esa fecha.
- **Eliminar una reserva generada**: también registra la fecha en
  `cancelled_dates` para que no se regenere.
- **Mover una reserva generada**: se desvincula de la serie y se registra la
  fecha original como anulada.
- **Finalizar la serie** (`DELETE /admin/recurring/:id`): cancela las
  ocurrencias futuras y conserva las pasadas.

## Variables de entorno

Backend (`booking-sport`):

- `RECURRING_JOB_TOKEN`: token compartido que autoriza el endpoint interno. Si
  no está definido, el endpoint responde `503`.

Batch (`booking-sport-batch`):

- `API_BASE_URL`: URL del backend (por defecto `http://localhost:8080`).
- `RECURRING_JOB_TOKEN`: el mismo token configurado en el backend.

GitHub Actions:

- Variable de repositorio `API_BASE_URL`.
- Secreto de repositorio `RECURRING_JOB_TOKEN`.

## Endpoints

- `POST /api/internal/jobs/recurring-generate` (header `X-Job-Token`):
  ejecución automática/idempotente.
- `POST /admin/recurring/generate`: ejecución manual desde el panel admin
  (requiere rol admin o administrar al menos un centro).

## Índices

- `bookings.recurring_id`
- `bookings (recurring_id, date)`
- `bookings (recurring_id, status, scheduled_at)`
- `recurring_reservations` único parcial por cancha/hora/minutos/día con
  `status = "active"`
- `job_locks.expires_at` con TTL
