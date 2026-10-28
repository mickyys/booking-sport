# Propuesta: convivencia de reservas legacy y UTC con Redis

**Estado:** propuesta de implementación.  
**Fecha:** 18 de octubre de 2026.  
**Alcance:** backend `booking-sport`, contratos de API y cambios necesarios en los clientes.

## 1. Objetivo y decisiones acordadas

Migrar gradualmente las reservas con fecha, hora y minutos separados a un modelo de intervalos UTC, manteniendo operativos los clientes y datos actuales.

Decisiones:

- El cliente verá **solo el horario del centro deportivo**, independientemente de la zona de su dispositivo.
- **Redis será la autoridad de exclusión** para bloqueos temporales y ocupaciones confirmadas.
- MongoDB conservará reservas, pagos e historial. Al finalizar la transición, no será la autoridad de bloqueo de horarios.
- La migración será por etapas y por centro, con lectura compatible y escritura de ambos formatos.
- La exclusión se aplicará a intervalos completos, no solamente a la hora de inicio.

## 2. Diagnóstico del código actual

La revisión se basa en el código del repositorio; no se inspeccionaron datos ni índices desplegados en producción.

| Componente | Situación observada |
|---|---|
| `internal/domain/entities.go` | `Booking` ya contiene `date`, `hour`, `minutes`, `duration_minutes`, `local_date`, `timezone` y `scheduled_at`. `SlotHold` también tiene campos temporales derivados. |
| `internal/domain/location.go` | Normalización y límites diarios fijados a `America/Santiago`. Existe validación de horas inexistentes y ambiguas para Santiago. Si falla la carga de la zona, se utiliza UTC silenciosamente. |
| `internal/app/booking_usecase.go` | `ClaimOrRenewSlot` administra holds de 15 minutos en MongoDB. Hay validación de solapamientos de confirmadas por día, separada de la escritura. Algunos errores de consulta se ignoran. |
| `internal/infra/mongo/slot_hold_repository.go` | Adquisición mediante inserción con índice único; renovación y limpieza de holds vencidos. |
| `internal/infra/mongo/indexes.go` | Índices únicos por inicio para `confirmed` y `pending`, por separado; TTL para `slot_holds`. No constituyen una restricción general de solapamiento de intervalos. |
| `internal/app/mp_payment_usecase.go` | Hay varias vías de confirmación y sincronización que deben converger en un único protocolo de ocupación. |
| `internal/app/schedule_resolver.go` | Conviven `schedule_rules` y horarios legacy. Las reglas se interpretan en Santiago. |
| `cmd/migrate-booking-schedule-fields/main.go` | Completa campos cuando falta `local_date`, utiliza Santiago y modifica también `date`; no tiene simulación ni completa todos los posibles documentos parcialmente migrados. |
| `go.mod`, `docker-compose.yml` | No se encontró integración con Redis. |

### Hallazgos a considerar antes del corte

1. Dos reservas con diferente inicio pueden solaparse aunque ambas pasen los índices únicos actuales.
2. Una consulta previa seguida de una escritura no constituye una adquisición atómica.
3. Los índices de `pending` y `confirmed` separados no garantizan exclusión entre ambos estados.
4. En `CancelBooking` se reconstruye el inicio con minuto cero, mientras otras rutas consideran `minutes`.
5. Algunas proyecciones de listados no devuelven todos los campos temporales nuevos ni los minutos.
6. `ensureUserIndexes` declara índices con nombres de `slot_holds` sobre `users`. Verificar los índices realmente desplegados y corregir su ubicación antes de utilizarlos como referencia de migración.
7. La expiración TTL de MongoDB es limpieza eventual: el vencimiento debe comprobarse explícitamente, sin depender de cuándo se borra el documento.

## 3. Modelo temporal objetivo

Se reutiliza `scheduled_at` como inicio canónico para evitar un segundo campo equivalente.

| Campo | Tipo | Semántica |
|---|---|---|
| `scheduled_at` | BSON Date / `time.Time` | Inicio absoluto. Serialización JSON en RFC 3339 UTC, con `Z`. |
| `ends_at` | BSON Date / `time.Time` | Fin absoluto y exclusivo. |
| `timezone` | string IANA | Zona del centro capturada al crear la reserva. |
| `local_date` | string `YYYY-MM-DD` | Fecha civil de inicio en la zona del centro. |
| `duration_minutes` | entero positivo | Duración transcurrida real entre inicio y fin. |
| `schedule_schema_version` | entero | Versión del modelo temporal; ausencia significa legacy. |
| `date`, `hour`, `minutes` | tipos actuales | Proyección de compatibilidad para clientes y consultas legacy. |

No reutilizar `version`: ya existe y debe permanecer separado del versionado temporal.

Ejemplo de representación JSON:

```json
{
  "scheduled_at": "2026-07-16T02:30:00Z",
  "ends_at": "2026-07-16T03:30:00Z",
  "timezone": "America/Santiago",
  "local_date": "2026-07-15",
  "hour": 22,
  "minutes": 30,
  "duration_minutes": 60,
  "schedule_schema_version": 2
}
```

MongoDB almacena BSON Date como un instante, no como una zona horaria. La zona IANA debe conservarse por separado. En Go, normalizar los instantes con `.UTC()` antes de persistir y serializar.

### Invariantes

- `scheduled_at < ends_at`.
- Los intervalos se representan como `[inicio, fin)`; dos reservas consecutivas son válidas.
- `ends_at = scheduled_at + duration_minutes` minutos transcurridos.
- La zona se obtiene del centro en el servidor; no se confía en una zona arbitraria enviada por el cliente.
- Los campos locales deben corresponder al instante canónico en `timezone`.
- Los formatos contradictorios en una solicitud se rechazan; no se elige uno silenciosamente.
- Los nuevos contratos trabajan con precisión de minutos y rechazan segundos o fracciones no soportados.
- La duración faltante o cero en documentos legacy se interpreta inicialmente como 60 minutos, conforme al comportamiento actual, y se registra esa inferencia en el reporte de migración.
- Una reserva existente mantiene su zona e instante aunque se cambie posteriormente la configuración del centro.

## 4. Zona del centro y presentación

Añadir `timezone` a `SportCenter` y a los contratos públicos necesarios para renderizar la agenda.

Para centros históricos verificados, completar `America/Santiago`. No deducir una zona a partir de un offset fijo ni asumir que todo Chile comparte necesariamente la misma zona.

### Reglas de implementación

1. Generalizar los helpers temporales para recibir una zona IANA explícita.
2. Mantener wrappers de Santiago para rutas legacy durante la transición.
3. Garantizar una base de zonas disponible en el contenedor, por ejemplo mediante `time/tzdata` o un paquete de sistema administrado.
4. Fallar explícitamente si una zona configurada es inválida o no puede cargarse.
5. Para un día local, convertir el inicio civil y el del día siguiente a UTC; no sumar siempre 24 horas.
6. Las reglas semanales y sus precios continúan expresándose en horario local del centro.

### Cambios de horario estacional

- **Hora inexistente:** no generar esa franja; una solicitud manual debe recibir un error de validación.
- **Hora repetida:** identificar la ocurrencia mediante un instante UTC o identificador de franja que incluya su ocurrencia. En el contrato legacy sin desambiguación, rechazar la hora ambigua.
- La comprobación genérica de ambigüedad no debe limitarse al desplazamiento de una hora que actualmente utiliza el helper de Santiago; hay zonas con otras transiciones.
- Una duración significa tiempo transcurrido. Si el negocio necesita duración de reloj civil, deberá modelarse como una política distinta.

### Interfaz, correos y notificaciones

Convertir `scheduled_at` y `ends_at` con la zona del centro o la zona histórica de la reserva. Mostrar, por ejemplo:

> 15 de julio, 22:30–23:30 · Hora del centro · America/Santiago

No utilizar la zona del dispositivo como predeterminada. No convertir `local_date` a un objeto fecha UTC para luego renderizarlo en la zona del navegador. En web, especificar `timeZone` al formatear instantes; en aplicaciones móviles, utilizar una biblioteca compatible con zonas IANA.

Las cancelaciones, recordatorios y clasificación de reservas pasadas deben comparar instantes canónicos. Definir explícitamente si “pasada” significa iniciada o finalizada y aplicarlo de manera uniforme.

## 5. Compatibilidad de contratos

### Lectura

- Documento versión 2 completo: utilizar `scheduled_at` y `ends_at` como autoridad.
- Documento legacy: resolver fecha civil, hora y minutos con el adaptador histórico y la zona verificada del centro.
- Documento parcialmente migrado: validar consistencia; no recalcular un documento versión 2 desde campos legacy.
- Centralizar la adaptación y reutilizarla en detalle, listados, disponibilidad, dashboard, pagos y notificaciones.

### Escritura

Dos entradas compatibles convergen en un único comando interno de reserva:

1. **Contrato actual:** fecha y hora separadas; conservar exactamente la semántica de fecha de sus handlers actuales.
2. **Contrato nuevo:** `scheduled_at` RFC 3339 con zona u offset explícito, y `duration_minutes`, preferiblemente a partir de una franja emitida por el servidor.

El servidor obtiene el centro, valida el horario y construye un intervalo UTC. En una misma escritura del documento MongoDB persiste el intervalo y su proyección legacy.

No aceptar timestamps sin offset. Si llegan `ends_at` y duración, exigir que coincidan. Si llegan simultáneamente ambos formatos, validar equivalencia o devolver un error de validación.

Mantener `court_id` durante esta migración para no acoplarla a la evolución de `courts` hacia `spaces`.

### Disponibilidad por día

Para un día local cuyo rango UTC es `[dayStart, nextDayStart)`, incluir cualquier ocupación que cumpla:

```text
scheduled_at < nextDayStart AND ends_at > dayStart
```

Esto incluye reservas que comenzaron el día anterior y siguen ocupando el espacio. Consultar solamente `local_date` no cubre ese caso.

## 6. Redis como autoridad de exclusión

### Estructura propuesta

Mantener un registro de intervalos por espacio, con un sorted set por inicio y un hash de metadatos:

```text
booking:{centerID:spaceID}:intervals
booking:{centerID:spaceID}:metadata
booking:{centerID:spaceID}:operations
```

El prefijo debe incluir el entorno real de despliegue. Las llaves del mismo espacio compartirán etiqueta de Redis Cluster para que un script Lua opere sobre la misma partición.

Cada ocupación contiene:

- `occupancy_id`, `booking_id` o identificador de operación preasignado.
- Inicio y fin UTC en milisegundos Unix.
- Propietario autenticado o sesión de invitado validada.
- Token aleatorio secreto de propiedad; no basta con conocer el `user_id`.
- Estado y versión de la operación.
- `expires_at` para holds temporales.

No almacenar datos personales del cliente en las claves.

### Estados

| Estado | Significado | Liberación |
|---|---|---|
| `held` | Checkout temporal, inicialmente de 15 minutos. | Vencimiento comprobado atómicamente o cancelación del propietario. |
| `committing` | Escritura en MongoDB iniciada o con resultado pendiente de resolver. | Reconciliación explícita; nunca por el TTL del checkout. |
| `booked` | Reserva confirmada persistida. | Cancelación, cambio de horario o limpieza después de su fin. |

`committing` es un estado operativo de Redis y no obliga a añadir un estado comercial equivalente a `BookingStatus`.

### Operaciones atómicas

Implementar scripts Lua para adquirir, renovar, iniciar confirmación, finalizar confirmación y liberar. Cada operación verifica los metadatos y la identidad/versiones esperadas antes de mutar.

La adquisición debe:

1. Verificar que el espacio está habilitado y reconciliado para escribir.
2. Obtener la hora del servidor Redis para evaluar vencimientos.
3. Ignorar o limpiar holds realmente vencidos.
4. Buscar candidatos cuyo inicio sea menor al fin solicitado.
5. Rechazar si un candidato vigente tiene `existingEnd > requestedStart`.
6. Registrar el nuevo intervalo y sus metadatos en la misma ejecución.

La fórmula de conflicto es:

```text
existingStart < requestedEnd AND existingEnd > requestedStart
```

Una clave por hora de inicio o un simple `SET NX PX` no detectan por sí solos todos los solapamientos.

### Expiración y tamaño

- Los metadatos de `held` incluyen vencimiento lógico; las operaciones lo consultan sin depender de eventos de expiración.
- Los eventos Redis de expiración pueden utilizarse para observación, no como requisito de integridad.
- Un reconciliador marca pendientes expirados en MongoDB de forma idempotente.
- Conservar ocupaciones confirmadas hasta su fin y una ventana operativa definida; luego depurarlas.
- Acotar el horizonte de reservas para mantener pequeño el conjunto por espacio. Medir candidatos y latencia Lua antes de considerar particiones por fecha.
- No aplicar TTL global a un registro que contiene ocupaciones confirmadas futuras.

## 7. Protocolo de reserva y persistencia

No existe una transacción atómica entre Redis y MongoDB. La propuesta utiliza estados intermedios conservadores e idempotencia para resolver los fallos entre sistemas.

### Creación de checkout

1. Validar identidad, centro, reglas, recurrencias, bloqueos administrativos, precio y duración.
2. Convertir la entrada a intervalo UTC.
3. Preasignar identificadores y asociar una clave de idempotencia al comando y su contenido.
4. Adquirir `held` atómicamente en Redis.
5. Persistir la reserva pendiente y su `occupancy_id` en MongoDB.
6. Si falla definitivamente la escritura, liberar solo el hold propio. Si el resultado es desconocido, resolver por identificador antes de compensar.

Si un hold vence antes de completar la creación del pendiente, ese documento no habilita por sí solo la confirmación: será necesario validar o readquirir la ocupación.

### Confirmación

1. Verificar el pago con el proveedor y resolver el resultado comercial.
2. Cambiar atómicamente el hold propio y vigente a `committing`.
3. Escribir en MongoDB mediante actualización condicional por estado/versión y operación esperada.
4. Ante éxito, convertir la ocupación a `booked` en Redis.
5. Ante fallo definitivo, compensar según el estado persistido.
6. Ante timeout o resultado desconocido, conservar `committing` y reconciliar consultando MongoDB.

Un trabajador pausado no debe poder reutilizar un token reemplazado. Antes de readquirir o compensar una operación incierta hay que resolver si su escritura quedó aplicada y evitar que una escritura anterior siga ejecutándose. Si no puede determinarse, mantener el espacio bloqueado para esa operación hasta resolverla.

### Cancelación y cambios

- Cancelar en MongoDB mediante una transición idempotente y después liberar Redis. Si falla la liberación, queda una ocupación conservadora que el reconciliador elimina.
- Un cambio de horario adquiere primero el intervalo nuevo, persiste el cambio y después libera el anterior; los estados intermedios deben conservar ambos hasta resolver la escritura.
- Las reservas internas deben usar el mismo coordinador que las públicas.
- Los lotes deben definir si admiten resultado parcial. Una operación sobre varios espacios no es atómica en Redis Cluster con etiquetas distintas; usar una saga con compensación o limitar la atomicidad al espacio.

### Pagos tardíos e idempotencia

- Si el hold ya venció, readquirir el intervalo mediante el mismo protocolo antes de confirmar.
- Si el intervalo está ocupado, registrar el pago recibido sin confirmar una reserva incompatible y generar una compensación/reembolso idempotente.
- Un webhook repetido no debe duplicar reservas, cobros contabilizados, reembolsos ni notificaciones.
- Todas las vías de confirmación de `mp_payment_usecase.go`, incluidas sincronizaciones y simulaciones, deben utilizar el coordinador.
- La clave de idempotencia se asocia al solicitante y a un hash del comando; reutilizarla con otro contenido produce conflicto.
- Conservar el resultado durable de la operación en MongoDB permite resolver reintentos incluso si expiró su caché Redis. Esto no constituye un bloqueo de horarios en MongoDB.

## 8. Reglas, recurrencias y bloqueos administrativos

Las recurrencias continúan definiéndose con día semanal, hora civil, duración y zona IANA. Cada ocurrencia se materializa en UTC con las reglas de su fecha, sin sumar semanas como duraciones fijas UTC.

- Materializar ocurrencias dentro del horizonte de reservas permitido.
- No abrir una fecha a nuevas reservas hasta que estén cargadas sus ocurrencias y bloqueos.
- Crear, terminar o editar una recurrencia debe coordinarse con las adquisiciones del espacio; una consulta previa a MongoDB no basta para evitar carreras.
- Aplicar el mismo protocolo a bloqueos administrativos que excluyen ocupación.
- Los cambios de reglas/precios deben versionarse y revalidarse antes de adquirir o confirmar, según la política comercial.
- Procesar cancelaciones de ocurrencias por fecha civil del centro.

## 9. Disponibilidad, recuperación y límites

### Límite explícito

Con exclusión únicamente en Redis, **no se puede garantizar ausencia absoluta de duplicados ante pérdida de escrituras reconocidas o promoción de una réplica atrasada**. AOF, replicación y confirmaciones adicionales reducen riesgo, pero no convierten Redis y MongoDB en una transacción distribuida ni eliminan todos los escenarios de failover.

La propuesta prioriza detener las escrituras cuando la autoridad de ocupación no sea confiable. Si se requiere una garantía absoluta incluso durante pérdidas de estado no detectadas, habrá que reconsiderar mantener una restricción durable de exclusión adicional.

### Configuración operativa

- Redis dedicado al registro de ocupación, con política `noeviction`.
- Persistencia AOF y política de sincronización elegida conforme al RPO aceptado; documentar la posible pérdida de datos.
- TLS, autenticación y ACL por entorno.
- Timeouts, límites de reintento y métricas del cliente Go.
- Capacidad dimensionada para el horizonte, holds y operaciones sin resolver.
- Detectar reinicios, restauraciones y cambios de autoridad antes de aceptar nuevas adquisiciones; no depender solamente de un flag volátil que también pueda perderse.
- MongoDB sigue siendo necesario para persistir una reserva y resolver estados inciertos.

### Redis no disponible o inconsistente

1. Rechazar nuevas adquisiciones, renovaciones y confirmaciones que no puedan coordinarse, con error temporal y política de reintento.
2. Permitir lecturas históricas desde MongoDB; señalar disponibilidad no confiable si corresponde.
3. No volver automáticamente al bloqueo MongoDB por solicitud.
4. Conservar o reintentar notificaciones de pago sin perder la evidencia del proveedor.

### Reconstrucción

1. Cerrar admisión de escrituras del ámbito afectado y drenar o detener escritores anteriores.
2. Resolver operaciones en curso con MongoDB y, cuando corresponda, el proveedor de pago.
3. Cargar confirmadas cuyo fin no haya pasado y pendientes con vencimiento vigente.
4. Materializar recurrencias y bloqueos del horizonte.
5. Detectar y reportar solapamientos existentes sin descartar datos silenciosamente.
6. Verificar conteos, intervalos e identificadores; publicar la nueva generación de ocupación.
7. Reabrir admisión únicamente con todos los escritores utilizando esa generación.

La reconstrucción conservadora puede prolongar bloqueos; no debe liberar una operación incierta únicamente por antigüedad.

## 10. Plan de migración por fases

### Fase 1 — Inventario y preparación

- Auditar formatos históricos de `date`, minutos ausentes, duraciones y campos parcialmente migrados.
- Verificar zona por centro y detectar duplicados/solapamientos existentes.
- Enumerar todas las rutas y procesos que crean, confirman, cambian o cancelan ocupación.
- Confirmar topología de MongoDB y requisitos operativos de Redis.
- Revisar índices reales, incluido el hallazgo en `ensureUserIndexes`.

**Salida:** inventario, reporte de inconsistencias y zonas verificadas.

### Fase 2 — Compatibilidad temporal

- Añadir zona por centro, `ends_at` y versión temporal.
- Implementar adaptadores de lectura y escritura conjunta del formato legacy y UTC.
- Corregir consultas, proyecciones, cancelación y notificaciones.
- Incorporar validación de esquema inicialmente compatible con ambas versiones.

**Salida:** clientes actuales operativos y nuevas reservas con campos consistentes. MongoDB conserva todavía la autoridad de exclusión.

### Fase 3 — Backfill

Crear o ampliar una herramienta con simulación por defecto, lotes, checkpoint, límite de carga y reporte de omitidos.

- Priorizar futuras confirmadas y pendientes vigentes; migrar después el historial.
- Preservar valores originales o un registro auditable antes de normalizar.
- No asumir que un `date` histórico a medianoche UTC equivale a la fecha local resultante de `.In(location)`; resolver conforme al contrato y procedencia de esos datos.
- Completar documentos parciales, no solamente aquellos sin `local_date`.
- Usar actualizaciones condicionales para no sobrescribir cambios concurrentes; registrar y reintentar documentos modificados.
- Separar horas inexistentes, ambiguas y solapamientos para resolución explícita.

El comando actual `migrate-booking-schedule-fields` sirve como antecedente, pero no debe ejecutarse sin revisar esas limitaciones.

**Salida:** cobertura temporal validada y ninguna reserva activa sin interpretación conocida.

### Fase 4 — Redis en observación

- Añadir cliente, scripts, registro de intervalos, reconciliador y métricas.
- Copiar ocupaciones y comparar decisiones de disponibilidad.
- Mantener MongoDB como autoridad mientras se comparan resultados.
- Ajustar capacidad y latencias con carga representativa.

**Salida:** divergencias explicadas y pruebas de recuperación aprobadas.

### Fase 5 — Corte controlado por centro

1. Desplegar primero código compatible en todas las instancias y procesos.
2. Pausar escrituras del centro, drenar operaciones y bloquear instancias antiguas.
3. Importar el estado final de confirmadas, pendientes vigentes, recurrencias y bloqueos.
4. Verificar consistencia y activar Redis como autoridad.
5. Reabrir el centro y observar las métricas.

Una flag por centro permite el despliegue gradual, pero su publicación y admisión deben estar coordinadas. Una instancia no puede decidir el modo con configuración local atrasada.

### Fase 6 — Retiro de exclusión MongoDB

- Tras validar el corte, retirar el uso operativo de `slot_holds`.
- Retirar de forma controlada los índices únicos de horario de reservas que corresponda al ámbito migrado; considerar que los índices actuales son globales y requerirán un diseño de filtros o esperar al corte global.
- Actualizar `EnsureIndexes` para que un reinicio no recree las restricciones retiradas.
- Conservar índices de consulta e integridad de identificadores, como `booking_code`.
- Mantener expiración y auditoría de pendientes sin utilizarlos como autoridad de disponibilidad.

**Salida:** Redis es la única autoridad de exclusión para todos los escritores migrados.

### Fase 7 — Retiro legacy

- Medir consumidores del contrato antiguo.
- Retirar fallbacks de lectura solo después de migrar los datos relevantes.
- Dejar de escribir campos legacy cuando ningún consumidor los necesite y termine la ventana de rollback.
- Documentar versión final de API y esquema.

## 11. Rollback

Durante la escritura conjunta, conservar compatibilidad permite volver a una versión preparada para ambos formatos.

Si Redis ya es autoritativo:

1. Pausar admisión y drenar operaciones.
2. Resolver `committing`, pagos y escrituras inciertas.
3. Reconstruir holds y ocupaciones en el mecanismo de destino.
4. Auditar duplicados antes de recrear índices únicos; los índices legacy por inicio no agregan una garantía general de intervalos.
5. Restaurar una versión de backend compatible con los datos generados.
6. Cambiar la autoridad coordinadamente y reabrir.

No utilizar un rollback automático por petición ni reactivar instancias antiguas que ignoren el coordinador.

## 12. Cambios de código previstos

| Archivo o área | Cambio propuesto |
|---|---|
| `internal/domain/entities.go` | Zona del centro, fin UTC, versión temporal y referencias de ocupación/operación. |
| `internal/domain/location.go` | Helpers por zona IANA, validación y conversiones canónicas. |
| `internal/app/booking_usecase.go` | Comando temporal único y coordinador para creación, renovación, cancelación y lotes. |
| `internal/app/mp_payment_usecase.go` | Confirmación única, idempotencia y pagos tardíos. |
| `internal/app/schedule_resolver.go` y recurrencias | Resolución por zona del centro y materialización coordinada. |
| Interfaces de `internal/app` | Puertos de ocupación, reloj, idempotencia y reconciliación. |
| Nueva infraestructura Redis | Cliente, scripts Lua, repositorio de ocupación y pruebas de integración. |
| `internal/infra/mongo/booking_repository.go` | Lectura compatible, transiciones condicionales y consultas UTC. |
| `internal/infra/mongo/indexes.go` | Índices de lectura, revisión de ubicación y retiro controlado de exclusión. |
| Handlers de reservas y disponibilidad | DTO compatibles, validación y campos temporales completos. |
| `cmd` | Migración reanudable, auditoría y reconstrucción de Redis. |
| `cmd/app/main.go`, `go.mod`, `docker-compose.yml` | Inyección de dependencias y configuración Redis. |
| Clientes web/móviles y plantillas | Renderizado explícito en zona del centro. |

Variables propuestas, aún no implementadas:

```text
REDIS_URL
REDIS_KEY_PREFIX
BOOKING_HOLD_TTL=15m
BOOKING_MAX_HOLD_LIFETIME
BOOKING_MAX_ADVANCE_DAYS
BOOKING_RECONCILE_INTERVAL
```

Definir los valores de horizonte y renovación máxima conforme a la política comercial. La autoridad por centro debe almacenarse y publicarse mediante un mecanismo coordinado, no solo una variable de entorno por instancia.

## 13. Pruebas y criterios de aceptación

### Tiempo y compatibilidad

- Ida y vuelta entre contrato legacy, UTC y presentación sin cambio de fecha civil.
- Reserva local nocturna cuyo día UTC sea el siguiente.
- Minutos distintos de cero y duraciones de 30, 60, 90 y 120 minutos.
- Intervalos que cruzan medianoche y límites de día de 23/25 horas.
- Horas inexistentes y repetidas; al menos otra zona con transición diferente de una hora.
- Dispositivo en otra zona que sigue mostrando la hora del centro.
- Migración reejecutable y segura frente a cambios concurrentes.
- Campos contradictorios rechazados y documentos parciales reportados.

### Exclusión y concurrencia

- Muchas solicitudes concurrentes al mismo intervalo: una sola adquisición vigente.
- Solapamientos parciales, contención completa y reservas consecutivas.
- Solicitudes públicas, administrativas, recurrentes y pagos sobre el mismo espacio.
- Renovación y liberación con token incorrecto rechazadas.
- Un propietario antiguo no puede liberar una ocupación readquirida.
- Vencimiento evaluado sin depender de notificaciones de expiración.

### Fallos e idempotencia

- Caída antes y después de cada paso entre Redis y MongoDB.
- Timeout de MongoDB con escritura aplicada y con escritura no aplicada.
- Checkout vencido durante una operación y trabajador pausado que intenta continuar.
- Webhooks duplicados, fuera de orden y pagos tardíos.
- Cancelación persistida cuya liberación Redis falla.
- Reinicio, pérdida de datos y failover Redis con réplica atrasada.
- Reconstrucción y rollback manteniendo admisión cerrada hasta validar.

### Observabilidad

Medir adquisiciones, conflictos, latencia Lua, holds vencidos, ocupaciones `committing`, antigüedad de operaciones inciertas, divergencias, errores por dependencia y duración de reconstrucción. Alertar ante evicciones, falta de memoria, errores de persistencia y escritores con generación incorrecta.

### Condiciones para finalizar

- Todas las reservas activas tienen intervalos y zona válidos.
- Todos los canales muestran exclusivamente el horario del centro.
- Todos los escritores de ocupación pasan por Redis.
- No quedan operaciones de confirmación que salten el coordinador.
- Se validaron recuperación y rollback con fallos inyectados.
- Se aceptó explícitamente el RPO y el límite de integridad de usar Redis como única autoridad de exclusión.
- Ningún índice o proceso legacy vuelve a activar silenciosamente otra autoridad de bloqueo.

## 14. Orden recomendado de implementación

1. Compatibilidad temporal y zona por centro.
2. Auditoría y backfill de reservas activas.
3. Coordinador Redis con pruebas de intervalos e idempotencia.
4. Integración de pagos, administración, recurrencias y bloqueos.
5. Reconciliación, recuperación y observabilidad.
6. Observación comparativa y corte por centro.
7. Retiro controlado de exclusión MongoDB y, posteriormente, del contrato legacy.
