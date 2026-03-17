# Instrucciones y Buenas Prácticas para Backend Monolítico en Go

## 1. Arquitectura recomendada
- **Framework Web**: Usar **Gin Gonic** para el manejo de rutas, middleware y respuestas JSON.
- **Arquitectura en capas (Layered Architecture)**:
  - **Interfaces (Presentación)**: Endpoints HTTP/REST para la app de reservas.
  - **Aplicación (Casos de uso)**: Lógica de reservas, pagos, reversas y notificaciones.
  - **Dominio**: Entidades (Usuario, Cancha, Reserva, Pago).
  - **Infraestructura**: Conexiones a MongoDB, Mailgun y Mercado Pago.

## 2. Organización del proyecto
    - /cmd/app         → main.go (punto de entrada)
    - /internal/domain → entidades y reglas de negocio
    - /internal/app    → casos de uso (reservas, pagos, emails)
    - /internal/infra  → adaptadores externos (Mongo, Mailgun, Mercado Pago)
    - /pkg             → utilidades (logging, middlewares)
    - /configs         → configuración (env, yaml)
    - /docs            → documentación  


## 3. Seguridad
- **Autenticación y autorización**:
  - Implementar JWT o sesiones seguras para usuarios.
  - Roles: administrador, usuario.
- **Protección de datos**:
  - Nunca almacenar contraseñas en texto plano (usar bcrypt).
  - Variables sensibles (API keys de Mailgun, Mercado Pago, Mongo URI) en `.env`.
- **Validación de entradas**:
  - Sanitizar datos recibidos en formularios y endpoints.
  - Limitar tamaño de payloads.
- **Comunicación segura**:
  - Usar HTTPS en producción.
  - Configurar CORS correctamente para la app cliente.
- **Auditoría y trazabilidad**:
  - Registrar operaciones críticas (pagos, reversas, creación/cancelación de reservas).
  - Logs con identificadores de usuario y operación.

## 4. Conexión a MongoDB
- Usar `mongo-go-driver`.
- Definir índices en colecciones críticas (ej. reservas por fecha).
- Implementar repositorios en `/internal/infra/mongo`.

## 5. Conexión a Mailgun
- Usar librería oficial de Mailgun para Go.
- Centralizar lógica de envío en `/internal/infra/mailgun`.
- Plantillas de email para confirmaciones y cancelaciones de reservas.

## 6. Conexión a Mercado Pago
- Usar SDK oficial de Mercado Pago para Go.
- Casos de uso:
  - **Pagos**: creación de preferencia de pago.
  - **Reversas**: manejo de devoluciones.
- Validar respuestas y firmar callbacks (webhooks).
- Registrar transacciones en MongoDB para trazabilidad.

## 7. Testing
- Unit tests para lógica de reservas y pagos.
- Integration tests para endpoints críticos.
- Mocking de Mailgun y Mercado Pago en pruebas.

## 8. Despliegue
- Compilar binario con `go build`.
- Dockerizar la aplicación.
- Pipeline CI/CD con pruebas automáticas y despliegue seguro.
- Monitoreo con Prometheus/Grafana.

## 9. Documentación
- GoDoc para funciones públicas.
- `README.md` con instrucciones de instalación y despliegue.
- Documentar endpoints REST (ej. con Swagger/OpenAPI).

## 10. Estándares de API
- **Framework**: Usar `github.com/gin-gonic/gin` para todos los endpoints.
- **Paginación**: Todos los endpoints `GET` que retornan un listado de valores (ej. `/api/sport-centers`, `/api/courts`) deben implementar paginación obligatoria.
  - Usar `c.Query("page")` y `c.Query("limit")` para obtener parámetros.
  - Deben retornar una estructura `PagedResponse` que incluya `data` (array), `total`, `page`, `limit` y `total_pages`.
  - Si no hay resultados, el campo `data` debe ser un array vacío `[]` y no `null`.
