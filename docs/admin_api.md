# Documentación API Backend - Panel de Administración

Esta documentación detalla los endpoints disponibles para el desarrollo de la aplicación móvil dirigida a los administradores de centros deportivos.

## Información General

- **Base URL:** `http://<domain>/api`
- **Autenticación:** Se requiere un token de Auth0 en el header de cada petición protegida.
  - `Authorization: Bearer <JWT_TOKEN>`

---

## 1. Dashboard y Estadísticas

### Obtener Datos del Dashboard
Retorna métricas de ventas y una lista paginada de reservas recientes para el administrador autenticado.

- **URL:** `/admin/dashboard`
- **Método:** `GET`
- **Parámetros de Consulta (Query Params):**
  - `page` (int, opcional): Número de página (defecto: 1).
  - `limit` (int, opcional): Cantidad de resultados por página (defecto: 10).
  - `date` (string, opcional): Filtrar por fecha `YYYY-MM-DD`.
  - `name` (string, opcional): Filtrar por nombre del cliente.
  - `code` (string, opcional): Filtrar por código de reserva.
  - `status` (string, opcional): Filtrar por estado (`pending`, `confirmed`, `cancelled`).
- **Respuesta (JSON):**
  ```json
  {
    "today_bookings_count": 10,
    "today_revenue": 150000,
    "today_online_revenue": 50000,
    "today_venue_revenue": 100000,
    "total_revenue": 2000000,
    "total_online_revenue": 800000,
    "total_venue_revenue": 1200000,
    "cancelled_count": 2,
    "recent_bookings": [
      {
        "id": "...",
        "customer_name": "Juan Perez",
        "customer_phone": "+569...",
        "booking_code": "ABC-123",
        "date": "2023-10-27T00:00:00Z",
        "hour": 18,
        "court_name": "Cancha 1",
        "status": "confirmed",
        "payment_method": "mercadopago",
        "price": 25000
      }
    ],
    "page": 1,
    "limit": 10,
    "total_pages": 5
  }
  ```

---

## 2. Gestión de Canchas

### Listar Canchas del Administrador
Lista todas las canchas de los centros deportivos asociados al usuario.

- **URL:** `/admin/courts`
- **Método:** `GET`
- **Respuesta (JSON):** Lista de objetos `Court`.

#### Ejemplo de respuesta

```json
[
  {
    "sport_center": {
      "id": "642f1a2b3c4d5e6f7a8b9c0d",
      "name": "Centro Deportivo Las Lomas",
      "slug": "las-lomas",
      "city": "Santiago",
      "address": "Av. Principal 123",
      "users": ["auth0|1234567890abcdef"],
      "courts_count": 2
    },
    "courts": [
      {
        "id": "742f1a2b3c4d5e6f7a8b9c1e",
        "name": "Cancha 1",
        "description": "Cancha sintética"
      },
      {
        "id": "842f1a2b3c4d5e6f7a8b9c2f",
        "name": "Cancha 2",
        "description": "Cancha de pasto"
      }
    ]
  }
]
```

### Crear una Nueva Cancha
- **URL:** `/admin/courts`
- **Método:** `POST`
- **Cuerpo (JSON):**
  ```json
  {
    "sport_center_id": "ID_DEL_CENTRO",
    "name": "Cancha Central",
    "description": "Cancha de pasto sintético"
  }
  ```
- **Respuesta (JSON):** Objeto `Court` creado.

### Actualizar una Cancha
- **URL:** `/admin/courts/:id`
- **Método:** `PUT`
- **Cuerpo (JSON):**
  ```json
  {
    "name": "Nuevo Nombre",
    "description": "Nueva Descripción"
  }
  ```
- **Respuesta (JSON):** `{"message": "Court updated successfully"}`

### Eliminar una Cancha
- **URL:** `/admin/courts/:id`
- **Método:** `DELETE`
- **Respuesta (JSON):** `{"message": "Court deleted successfully"}`

---

## 3. Gestión de Horarios y Disponibilidad

### Obtener Calendario con Detalles de Reservas (Vista de Agenda)
Este endpoint es el más importante para el administrador. Retorna todos los slots horarios y, si están ocupados, incluye el detalle del cliente y la reserva.

- **URL:** `/sport-centers/:id/schedules/bookings`
- **Método:** `GET`
- **Parámetros de Consulta:**
  - `date` (string, opcional): Fecha `YYYY-MM-DD` (defecto: hoy en `America/Santiago`).
  - `all` (bool, opcional): Si es `true`, incluye todos los slots (incluso bloqueados o pasados).
- **Respuesta (JSON):** Lista de canchas con sus respectivos slots. Cada slot puede tener un objeto `booking` asociado.

Además existe un endpoint protegido para administradores que no requiere el id del centro: devuelve la agenda de los centros asociados al usuario autenticado.

- **URL:** `/admin/sport-centers/schedules/bookings`
- **Método:** `GET`
- **Parámetros de Consulta:**
  - `date` (string, opcional): Fecha `YYYY-MM-DD` (defecto: hoy en `America/Santiago`).
  - `all` (bool, opcional): Si es `true`, incluye todos los slots (incluso bloqueados o pasados).
- **Autenticación:** `Authorization: Bearer <JWT_TOKEN>` (admin)
- **Respuesta (JSON):** Array de objetos por centro: `{ "sport_center": {...}, "schedules": [...] }`.

Ejemplo de respuesta (cuando el administrador gestiona 1 centro):

```json
[
  {
    "sport_center": { "id": "642f1a2b3c4d5e6f7a8b9c0d", "name": "Centro Deportivo Las Lomas", "slug": "las-lomas" },
    "schedules": [
      {
        "id": "742f1a2b3c4d5e6f7a8b9c1e",
        "name": "Cancha 1",
        "schedule": [
          { "hour": 9, "minutes": 0, "price": 20000, "status": "available", "payment_required": true },
          { "hour": 10, "minutes": 0, "price": 20000, "status": "booked", "booking_code": "ABC-123", "customer_name": "Juan Perez" }
        ]
      }
    ]
  }
]
```

### Configurar Horario Semanal (Masivo)
Configura todos los slots de una cancha de forma recurrente.

- **URL:** `/admin/courts/:id/schedule`
- **Método:** `PUT`
- **Cuerpo (JSON):**
  ```json
  [
    {
      "hour": 9,
      "minutes": 0,
      "price": 20000,
      "status": "available",
      "payment_required": true
    },
    ...
  ]
  ```
- **Respuesta:** `204 No Content`

### Actualizar un Slot Específico (Bloqueos o Cambios de Precio)
Se usa para bloquear una hora específica (marcar como `closed`) o cambiar el precio de un slot puntual.

- **URL:** `/admin/courts/:id/schedule/slot`
- **Método:** `PATCH`
- **Cuerpo (JSON):**
  ```json
  {
    "hour": 18,
    "minutes": 0,
    "price": 25000,
    "status": "closed",
    "payment_required": false
  }
  ```
- **Respuesta (JSON):** `{"message": "Schedule slot updated successfully"}`

---

## 4. Gestión de Reservas

### Ver Detalle de una Reserva
- **URL:** `/bookings/:id`
- **Método:** `GET`
- **Respuesta (JSON):**
  ```json
  {
    "booking_detail": { ... },
    "hours_until_match": 24,
    "can_cancel": true,
    "refund_percentage": 100,
    "max_refund_amount": 25000,
    "cancellation_policy": { ... }
  }
  ```

### Crear Reserva Interna (Manual)
Utilizado cuando el administrador recibe una reserva por fuera de la plataforma (teléfono, presencial).

- **URL:** `/admin/bookings/internal`
- **Método:** `POST`
- **Cuerpo (JSON):**
  ```json
  {
    "court_id": "...",
    "sport_center_id": "...",
    "date": "2023-10-30T00:00:00Z",
    "hour": 20,
    "price": 25000,
    "customer_name": "Pedro Picapiedra",
    "customer_phone": "987654321",
    "payment_method": "internal"
  }
  ```
- **Respuesta (JSON):** Objeto `Booking` creado.

### Cancelar Reserva
Cambia el estado de la reserva a `cancelled` y libera el slot.
- **URL:** `/bookings/:id/cancel`
- **Método:** `POST`
- **Respuesta (JSON):** `{"status": "cancelled"}`

### Eliminar Reserva (Físico)
Elimina permanentemente el registro de la reserva de la base de datos.
- **URL:** `/admin/bookings/:id`
- **Método:** `DELETE`
- **Respuesta (JSON):** `{"status": "deleted"}`

### Listar Series Recurrentes
- **URL:** `/admin/bookings/series`
- **Método:** `GET`
- **Respuesta (JSON):**
  ```json
  {
    "data": [
      {
        "series_id": "...",
        "customer_name": "Juan Perez",
        "customer_phone": "569...",
        "court_name": "Cancha 1",
        "hour": 18,
        "start_date": "...",
        "end_date": "...",
        "bookings_count": 4
      }
    ]
  }
  ```

### Eliminar una Serie Recurrente
Elimina todas las reservas futuras asociadas a una serie.
- **URL:** `/admin/bookings/series/:series_id`
- **Método:** `DELETE`
- **Respuesta (JSON):** `{"message": "Series deleted successfully"}`

---

## 6. Identificación de Administradores (Auth0)

Los endpoints del panel de administración requieren un token JWT válido emitido por Auth0.

- El backend espera el header: `Authorization: Bearer <JWT_TOKEN>`.
- El middleware de autenticación valida `iss` y `aud`, verifica la firma contra el JWKS de Auth0 y extrae el `sub` del token.
- El valor `sub` (subject) se guarda en el contexto de Gin como `user_id` y se usa para asociar acciones al usuario autenticado.

Flujo resumido:

1. El cliente obtiene un token desde Auth0 (con el `audience` correcto para la API).
2. Envía requests al backend incluyendo `Authorization: Bearer <JWT>`.
3. El backend valida el token y ejecuta `c.Set("user_id", claims["sub"])` (ver implementación en [pkg/auth/auth.go](pkg/auth/auth.go#L122)).
4. Para determinar si el usuario es administrador de un centro deportivo, el backend verifica si `user_id` aparece en la lista de administradores del centro (`center.Users`). Ejemplo de la comprobación en el backend: [internal/app/booking_usecase.go](internal/app/booking_usecase.go#L630).

Ejemplo de claims (payload decodificado) esperado en el JWT:

```json
{
  "iss": "https://<YOUR_AUTH0_DOMAIN>/",
  "sub": "auth0|1234567890abcdef",
  "aud": ["your-api-audience"],
  "iat": 1610000000,
  "exp": 1610003600,
  "scope": "openid profile email",
  "email": "admin@centro.cl"
}
```

Notas importantes:

- Asegúrate de que el `audience` configurado en el cliente de Auth0 coincida con el `audience` que el middleware espera.
- El backend NO asume que cualquier token válido pertenece a un administrador; valida la pertenencia comprobando la presencia del `sub` en `center.Users` antes de permitir acciones administrativas.
- Si quieres que se use un claim diferente para roles (por ejemplo `https://example.com/roles`), podemos extender el middleware para exponer ese claim y documentarlo aquí.

## 5. Configuración del Centro Deportivo

### Obtener Datos del Centro
- **URL:** `/admin/sport-centers/:id`
- **Método:** `GET`
- **Respuesta (JSON):** `{"center": { ... }}`

### Actualizar Información General
- **URL:** `/admin/sport-centers/:id`
- **Método:** `PUT`
- **Cuerpo (JSON):** Objeto `SportCenter` completo.
- **Respuesta (JSON):**
  ```json
  {
    "center": { ... },
    "cancellation_policy": {
      "hours": 3,
      "retention_percent": 10
    }
  }
  ```

### Actualizar Políticas y Slug (Ajustes rápidos)
- **URL:** `/admin/sport-centers/:id/settings`
- **Método:** `PATCH`
- **Cuerpo (JSON):**
  ```json
  {
    "slug": "nuevo-slug",
    "cancellation_hours": 3,
    "retention_percent": 10
  }
  ```
- **Respuesta (JSON):** `{"message": "Settings updated successfully"}`

---

## Glosario de Valores y Enums

### Métodos de Pago (`payment_method`)
- `mercadopago`: Pago realizado online mediante Mercado Pago.
- `fintoc`: Pago realizado online mediante Fintoc.
- `internal`: Reserva creada manualmente por el administrador desde el panel.
- `presencial` / `venue`: Pago que se realizará directamente en el centro deportivo.

### Tipos de Reserva y Estados de Slot
- **Reserva Online:** El slot pasa a `status: "booked"` y el `payment_method` es `mercadopago` o `fintoc`.
- **Reserva Interna:** El slot pasa a `status: "booked"` y el `payment_method` es `internal`.
- **Bloqueo de Administrador:** El slot tiene `status: "closed"`. No tiene una reserva asociada, simplemente no está disponible para el público.

### Estados de Reserva (`status`)
- `pending`: Reserva iniciada pero pago no confirmado.
- `confirmed`: Reserva válida y confirmada.
- `cancelled`: Reserva anulada (slot liberado).

### Estados de Slot (`status`)
- `available`: Libre para ser reservado.
- `booked`: Ocupado por una reserva.
- `closed`: Bloqueado manualmente (mantenimiento, clases, etc).
