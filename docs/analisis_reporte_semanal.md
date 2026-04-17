# Análisis: Reporte Semanal de Reservas

Este documento proporciona las especificaciones técnicas para implementar un sistema de reportes automáticos que se ejecutará como un Cron Job en un repositorio externo.

## 1. Planificación (Cron)
- **Ejecución:** Todos los Lunes a las 05:00 AM (America/Santiago).
- **Rango de Datos:** Lunes 00:00:00 a Domingo 23:59:59 de la semana inmediata anterior.
- **Zona Horaria:** Es mandatorio realizar los cálculos y consultas usando la zona horaria de Chile (`UTC-4` o `UTC-3` según horario de verano).

---

## 2. Consultas de Base de Datos (MongoDB)

### A. Listado de Destinatarios
Obtener todos los centros deportivos activos para procesar sus reportes.
```javascript
db.sport_centers.find({}, { name: 1, "contact.email": 1 })
```

### B. Consulta de Resumen (Cuerpo del HTML)
Esta consulta genera las estadísticas globales que se muestran directamente en el correo.

```javascript
// Rango de fechas calculado: [startDate, endDate]
db.bookings.aggregate([
  {
    $match: {
      sport_center_id: ObjectId("ID_CENTRO"),
      date: { $gte: startDate, $lte: endDate },
      status: { $in: ["confirmed", "cancelled"] }
    }
  },
  {
    $group: {
      _id: null,
      count_confirmed: { $sum: { $cond: [{ $eq: ["$status", "confirmed"] }, 1, 0] } },
      count_cancelled: { $sum: { $cond: [{ $eq: ["$status", "cancelled"] }, 1, 0] } },
      // Recaudación Online (Mercado Pago - Efectivamente pagado)
      revenue_online: {
        $sum: {
          $cond: [
            { $and: [
              { $eq: ["$status", "confirmed"] },
              { $eq: ["$payment_method", "mercadopago"] }
            ]},
            "$paid_amount",
            0
          ]
        }
      },
      // Recaudación Presencial (Pagado en cancha + Pendientes de pagos parciales)
      revenue_presencial: {
        $sum: {
          $cond: [
            { $and: [
              { $eq: ["$status", "confirmed"] },
              { $in: ["$payment_method", ["venue", "internal", "presencial"]] }
            ]},
            "$price", // Si es presencial total, el precio completo
            0
          ]
        }
      },
      revenue_pending_venue: {
        $sum: {
          $cond: [
            { $and: [
              { $eq: ["$status", "confirmed"] },
              { $eq: ["$is_partial_payment", true] },
              { $eq: ["$partial_payment_paid", false] }
            ]},
            "$pending_amount",
            0
          ]
        }
      }
    }
  }
])
```

### C. Consulta Detallada (Excel)
Proyecta los campos exactos requeridos para el archivo adjunto.

```javascript
db.bookings.aggregate([
  {
    $match: {
      sport_center_id: ObjectId("ID_CENTRO"),
      date: { $gte: startDate, $lte: endDate },
      status: { $in: ["confirmed", "cancelled"] }
    }
  },
  {
    $lookup: {
      from: "courts",
      localField: "court_id",
      foreignField: "_id",
      as: "court"
    }
  },
  { $unwind: "$court" },
  {
    $project: {
      _id: 0,
      "Fecha": { $dateToString: { format: "%d/%m/%Y", date: "$date", timezone: "America/Santiago" } },
      "Hora": { $concat: [{ $toString: "$hour" }, ":00"] },
      "Cancha": "$court.name",
      "Cliente": { $ifNull: ["$customer_name", "$guest_details.name"] },
      "Email": { $ifNull: ["$customer_email", "$guest_details.email"] },
      "Teléfono": { $ifNull: ["$customer_phone", "$guest_details.phone"] },
      "Código": "$booking_code",
      "Estado": "$status",
      "Método Pago": "$payment_method",
      "Total Reserva": "$price",
      "Pagado Online": { $ifNull: ["$paid_amount", 0] },
      "Pendiente en Cancha": { $ifNull: ["$pending_amount", 0] }
    }
  },
  { $sort: { "Fecha": 1, "Hora": 1 } }
])
```

---

## 3. Especificaciones del Reporte

### Estructura HTML (Email)
El correo debe ser profesional y visual, conteniendo:
1. **Resumen de Actividad:** Cantidad de reservas confirmadas vs canceladas.
2. **Resumen Financiero:**
   - Total Pagado Online (MP).
   - Total a Cobrar en Cancha (Incluye reservas presenciales y saldos de pagos parciales).
3. **Pie de página:** Recordatorio de que el detalle completo se encuentra en el Excel.

### Formato del Excel
- **Nombre del archivo:** `Reporte_Reservas_[NOMBRE_CENTRO]_[FECHA].xlsx`
- **Hojas:** Una sola hoja con la tabla de datos proyectada en la consulta C.

---

## 4. Recomendaciones Técnicas
- **Manejo de Errores:** Si un centro no tiene reservas, se recomienda enviar un correo informativo simplificado ("Sin actividad esta semana") o saltar el envío según preferencia del administrador.
- **Seguridad:** El proceso cron debe autenticarse contra MongoDB con permisos de solo lectura para las colecciones `sport_centers`, `bookings` y `courts`.
