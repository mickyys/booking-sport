# Configuración de New Relic para Logs

## Estado Actual

✅ El SDK de New Relic **ya está integrado** en el backend con:
- `github.com/newrelic/go-agent/v3` (v3.43.3)
- `github.com/newrelic/go-agent/v3/integrations/logcontext-v2/nrzap`
- `github.com/newrelic/go-agent/v3/integrations/nrgin`

✅ El logger (Zap) **ya está configurado** para enviar logs a New Relic mediante `nrzap`

✅ El middleware de New Relic **ya está registrado** en `main.go`

## Pasos para Habilitar el Envío de Logs

### 1. Obtener tu License Key de New Relic

1. Inicia sesión en https://one.newrelic.com
2. Ve a **Settings** → **API keys**
3. Copia tu **License Key** (User key o REST API key)

### 2. Configurar el archivo `.env`

Edita `/booking-sport/.env` y agrega tu license key:

```bash
NEW_RELIC_LICENSE_KEY=tu_license_key_aqui
NEW_RELIC_APP_NAME=ReservaYA-API-Dev
NEW_RELIC_ENABLED=true
ENVIRONMENT=development  # o production
```

### 3. Reiniciar la aplicación

```bash
cd booking-sport
go run cmd/app/main.go
```

### 4. Verificar en New Relic

1. Ve a https://one.newrelic.com
2. Navega a **APM** → **ReservaYA-API-Dev**
3. Selecciona **Logs** para ver los logs en tiempo real

## Configuración Actual del Código

### En `pkg/newrelic/client.go:34-41`
```go
options := []newrelic.ConfigOption{
    newrelic.ConfigLicense(cfg.LicenseKey),
    newrelic.ConfigAppName(cfg.AppName),
    newrelic.ConfigDistributedTracerEnabled(true),
    newrelic.ConfigCustomInsightsEventsMaxSamplesStored(10000),
    newrelic.ConfigAppLogEnabled(true),           // ✅ Logs habilitados
    newrelic.ConfigAppLogForwardingEnabled(true), // ✅ Logs enviados a New Relic
    newrelic.ConfigAIMonitoringEnabled(true),     // ✅ Monitoreo de IA
}
```

### En `pkg/logger/logger.go:45-101`
```go
// Reconfigura el logger después de inicializar New Relic
func ReconfigureForNewRelic(cfg Config, nrApp *newrelic.Application) {
    nrCore, err := nrzap.WrapBackgroundCore(core, nrApp)
    // ✅ Core de New Relic envuelto para logs en contexto
}
```

### En `cmd/app/main.go:38-49`
```go
nrApp, err := nr.Init(nrConfig)
if nrApp != nil {
    log = logger.ReconfigureForNewRelic(logConfig, nrApp)
    log.Infow("new_relic_logs_enabled")
}
```

## Cómo se Envían los Logs

### 1. **Logs Automáticos de HTTP**
El middleware de New Relic captura automáticamente:
- Todas las peticiones HTTP
- Tiempos de respuesta
- Códigos de estado
- Errores

### 2. **Logs Personalizados**
Usa el logger en cualquier parte del código:

```go
// En Handlers
log := h.baseHandler.GetLogger(c)
log.Infow("booking_created", "booking_id", booking.ID)

// En Use Cases
log := logger.FromContext(ctx)
log.Errorw("payment_failed", "error", err)

// En Repositories
log := logger.FromContext(ctx)
log.Debugw("mongo_query", "collection", "bookings")
```

### 3. **Logs con Contexto de Transacción**
Cuando usas `logger.FromContext(ctx)` dentro de una petición HTTP:
- Los logs se asocian automáticamente a la transacción de New Relic
- Se incluye el `trace_id` para distributed tracing
- Los logs aparecen vinculados al request en el dashboard

## Configuración por Ambiente

### Development (`ENVIRONMENT=development`)
- New Relic se **deshabilita** automáticamente (línea 45-47 en `client.go`)
- Los logs solo se escriben en stdout
- Ideal para desarrollo local

### Production (`ENVIRONMENT=production`)
- New Relic se **habilita** completamente
- Todos los logs se envían a New Relic
- Distributed tracing activado

## Comandos Útiles

### Ver logs en tiempo real (local)
```bash
go run cmd/app/main.go | jq
```

### Ver logs en producción (Docker)
```bash
docker logs -f booking-sport-api
```

### Buscar logs en New Relic (NRQL)
```sql
-- Todos los logs de la aplicación
SELECT * FROM Log WHERE appName = 'ReservaYA-API-Prod'

-- Logs de error
SELECT * FROM Log WHERE level = 'ERROR' AND appName = 'ReservaYA-API-Prod'

-- Logs por endpoint
SELECT count(*) FROM Log WHERE appName = 'ReservaYA-API-Prod' FACET http.route

-- Logs de booking
SELECT * FROM Log WHERE message LIKE '%booking%' LIMIT 100
```

## Troubleshooting

### Los logs no aparecen en New Relic

1. **Verifica la license key:**
   ```bash
   grep NEW_RELIC_LICENSE_KEY .env
   ```

2. **Verifica que New Relic esté inicializado:**
   Busca en los logs de startup:
   ```
   {"event":"new_relic_logs_enabled"}
   ```

3. **Verifica el environment:**
   Si `ENVIRONMENT=development`, New Relic está deshabilitado por diseño.
   Cambia a `production` para habilitarlo.

4. **Verifica la conectividad:**
   New Relic necesita salida a internet:
   - `log-api.newrelic.com`
   - `rpm-api.newrelic.com`

### Logs duplicados (local + New Relic)

Esto es esperado. El logger:
- Escribe en stdout (para ver en consola/Docker logs)
- Envía a New Relic (para monitoring)

### Alto volumen de logs

Ajusta el nivel de logging en `.env`:
```bash
LOG_LEVEL=warn  # Solo warnings y errores
```

## Recursos

- [New Relic Go Agent Docs](https://docs.newrelic.com/docs/apm/agents/go-agent/)
- [New Relic Logs](https://docs.newrelic.com/docs/logs/intro-logs/)
- [nrzap Integration](https://github.com/newrelic/go-agent/tree/main/v3/integrations/logcontext-v2/nrzap)
- [LOGGING_GUIDE.md](./LOGGING_GUIDE.md) - Guía completa de logging
