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
- **Respuesta:** Lista de objetos `Court`.

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

### Eliminar una Cancha
- **URL:** `/admin/courts/:id`
- **Método:** `DELETE`

---

## 3. Gestión de Horarios y Disponibilidad

### Obtener Calendario con Detalles de Reservas
Este endpoint es crucial para la vista de agenda. Muestra los slots horarios y, si están ocupados, incluye la información del cliente.

- **URL:** `/sport-centers/:id/schedules/bookings`
- **Método:** `GET`
- **Parámetros de Consulta:**
  - `date` (string, opcional): Fecha `YYYY-MM-DD` (defecto: hoy).
  - `all` (bool, opcional): Si es `true`, incluye slots cerrados o pasados.
- **Respuesta:** Lista de canchas con sus respectivos slots y detalles de reserva.

### Configurar Horario Semanal (Masivo)
Configura todos los slots de una cancha.

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

### Actualizar un Slot Específico
Modifica un solo horario sin afectar el resto.

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

---

## 4. Gestión de Reservas

### Ver Detalle de una Reserva
- **URL:** `/bookings/:id`
- **Método:** `GET`

### Crear Reserva Interna (Manual)
Permite al administrador registrar una reserva tomada por teléfono o presencialmente.

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

### Cancelar Reserva
- **URL:** `/bookings/:id/cancel`
- **Método:** `POST`

### Eliminar Reserva (Físico)
Elimina el registro de la base de datos (usar con precaución).
- **URL:** `/admin/bookings/:id`
- **Método:** `DELETE`

### Listar Series Recurrentes
- **URL:** `/admin/bookings/series`
- **Método:** `GET`

### Eliminar una Serie Recurrente
- **URL:** `/admin/bookings/series/:series_id`
- **Método:** `DELETE`

---

## 5. Configuración del Centro Deportivo

### Obtener Datos del Centro
- **URL:** `/admin/sport-centers/:id`
- **Método:** `GET`

### Actualizar Información General
- **URL:** `/admin/sport-centers/:id`
- **Método:** `PUT`
- **Cuerpo (JSON):** Mismo objeto que la creación (Nombre, Dirección, Contacto, etc).

### Actualizar Políticas y Slug
Permite cambiar el slug (URL amigable) y las políticas de cancelación.

- **URL:** `/admin/sport-centers/:id/settings`
- **Método:** `PATCH`
- **Cuerpo (JSON):**
  ```json
  {
    "slug": "nuevo-nombre-centro",
    "cancellation_hours": 4,
    "retention_percent": 15
  }
  ```

---

## Estados y Enums

### Estados de Reserva (`status`)
- `pending`: Pendiente de pago.
- `confirmed`: Confirmada.
- `cancelled`: Cancelada.

### Estados de Slot (`status`)
- `available`: Disponible para reserva.
- `booked`: Reservado.
- `closed`: Bloqueado por el administrador.
