# Logging & New Relic Integration Guide

## Overview

This guide documents the structured logging and New Relic APM integration implemented in the booking-sport API.

## Architecture

### Logging Stack
- **Logger**: [Zap](https://github.com/uber-go/zap) - High-performance structured logging
- **Format**: JSON (production) or Console (development)
- **Levels**: DEBUG, INFO, WARN, ERROR
- **Context**: Trace IDs for distributed tracing

### APM Stack
- **Provider**: New Relic One
- **Agent**: Official New Relic Go Agent v3
- **Features**: Distributed tracing, error tracking, performance monitoring

## Configuration

### Environment Variables

```bash
# New Relic
NEW_RELIC_LICENSE_KEY=<your-license-key>
NEW_RELIC_APP_NAME=ReservaYA-API-Prod
NEW_RELIC_ENABLED=true

# Logging
LOG_LEVEL=info                    # debug, info, warn, error
LOG_FORMAT=json                   # json or console
SERVICE_VERSION=1.0.0
ENVIRONMENT=production

# Gin
GIN_MODE=release                  # release or debug
```

### Setup Steps

1. **Get New Relic License Key**
   - Login to https://one.newrelic.com
   - Navigate to Settings → API keys
   - Copy your license key

2. **Configure Environment**
   ```bash
   cp .env.example .env
   # Edit .env with your New Relic license key
   ```

3. **Deploy**
   ```bash
   docker-compose up -d
   ```

## Usage

### In Handlers

```go
func (h *BookingHandler) CreateBooking(c *gin.Context) {
    log := h.baseHandler.GetLogger(c)
    
    // Log with context (includes trace_id, user_id automatically)
    log.Infow("booking_create_started",
        "court_id", booking.CourtID.Hex(),
        "date", booking.Date,
        "amount", booking.FinalPrice,
    )
    
    // ... business logic ...
    
    if err != nil {
        log.Errorw("booking_create_failed", "error", err)
        return
    }
    
    log.Infow("booking_create_success",
        "booking_code", booking.BookingCode,
    )
}
```

### In Use Cases

```go
func (uc *BookingUseCase) CreateBooking(ctx context.Context, booking *domain.Booking) error {
    log := logger.FromContext(ctx)
    
    log.Infow("booking_validation_started",
        "user_id", logger.GetUserID(ctx),
    )
    
    // ... business logic ...
}
```

### In Repositories

```go
func (r *BookingRepository) Create(ctx context.Context, booking *domain.Booking) error {
    log := logger.FromContext(ctx)
    
    start := time.Now()
    // ... database operation ...
    duration := time.Since(start)
    
    if duration > 100*time.Millisecond {
        log.Warnw("mongo_query_slow",
            "collection", "bookings",
            "duration_ms", duration.Milliseconds(),
        )
    }
    
    return nil
}
```

## Log Structure

### Standard Fields (All Logs)
```json
{
  "timestamp": "2026-04-29T17:00:00Z",
  "level": "INFO",
  "service": "booking-sport-api",
  "version": "1.0.0",
  "environment": "production",
  "event": "booking_create_started"
}
```

### Request Context Fields
```json
{
  "trace_id": "abc123-def456-ghi789",
  "span_id": "xyz789",
  "user_id": "auth0|123456",
  "method": "POST",
  "path": "/api/bookings",
  "status_code": 201,
  "duration_ms": 145
}
```

### Business Event Fields
```json
{
  "booking_code": "4cbd7d66",
  "court_id": "69b5f340f1d7053e707555b8",
  "center_id": "69b600d4989ae3a65d3e01af",
  "date": "2026-03-27",
  "hour": 23,
  "final_price": 25000,
  "payment_method": "mercadopago"
}
```

## Data Masking

Sensitive data is automatically masked:

- **Emails**: `juan.perez@gmail.com` → `ju***@gmail.com`
- **API Keys**: `sk_test_abc123xyz` → `sk_t***123xyz`
- **Phones**: `+56912345678` → `+56912***5678`
- **Mongo URI**: `mongodb://user:pass@host` → `***@host`

## New Relic Dashboards

### Key Metrics to Monitor

1. **API Performance**
   - Throughput (requests/min)
   - Latency (p50, p95, p99)
   - Error rate by endpoint
   - Status code distribution

2. **Business Metrics**
   - Bookings created per hour
   - Payment success rate
   - Cancellation rate
   - Average booking value

3. **External Dependencies**
   - MongoDB query latency
   - Mailgun email delivery
   - Firebase push notifications
   - MercadoPago/Fintoc payments

### NRQL Queries

```sql
-- Error rate by endpoint
SELECT count(*) FROM Transaction 
WHERE appName = 'ReservaYA-API-Prod' 
FACET name 
SINCE 1 hour ago

-- Slow transactions
SELECT duration FROM Transaction 
WHERE appName = 'ReservaYA-API-Prod' AND duration > 1000 
SINCE 1 hour ago

-- Custom business events
SELECT count(*) FROM BookingCreated 
SINCE 1 day ago 
FACET payment_method

-- Database performance
SELECT duration FROM DatastoreStatement 
WHERE appName = 'ReservaYA-API-Prod' 
SINCE 1 hour ago
```

## Alerting

### Recommended Alerts

1. **High Error Rate**
   - Condition: Error rate > 5% in 5 minutes
   - Severity: Critical

2. **High Latency**
   - Condition: p95 latency > 500ms in 5 minutes
   - Severity: Warning

3. **Service Down**
   - Condition: No health check requests in 2 minutes
   - Severity: Critical

4. **External API Failures**
   - Condition: Mailgun/Firebase error rate > 10%
   - Severity: Warning

## Health Endpoints

### GET /health
```json
{
  "status": "healthy",
  "service": "booking-sport-api",
  "version": "1.0.0",
  "timestamp": "2026-04-29T17:00:00Z"
}
```

### GET /ready
```json
{
  "status": "ready",
  "service": "booking-sport-api"
}
```

## Distributed Tracing

Every request gets a unique `X-Trace-ID` header that is:
1. Generated at the API gateway
2. Propagated through all layers (handler → usecase → repository)
3. Included in all log statements
4. Sent to New Relic for trace visualization

### Example Trace Flow
```
GET /api/bookings/123
├─ Handler: GetBookingDetail (15ms)
│  ├─ UseCase: GetByID (5ms)
│  │  └─ Repository: FindOne (3ms)
│  ├─ UseCase: GetCourtByID (7ms)
│  │  └─ Repository: FindOne (5ms)
│  └─ UseCase: GetSportCenterByID (3ms)
│     └─ Repository: FindOne (2ms)
└─ Response: 200 OK
```

## Performance Impact

- **Zap Logger**: <1% overhead (vs standard log)
- **JSON Formatting**: ~2% overhead
- **Distributed Tracing**: ~1-2ms per request
- **New Relic Agent**: ~3-5% overhead total

## Troubleshooting

### No logs appearing in New Relic

1. Check `NEW_RELIC_LICENSE_KEY` is correct
2. Verify `NEW_RELIC_ENABLED=true`
3. Check network connectivity to New Relic
4. Look for initialization errors in startup logs

### Logs not showing trace IDs

1. Ensure middleware is registered in `main.go`
2. Check that `logger.FromContext(ctx)` is used
3. Verify trace ID is propagated in context

### High memory usage

1. Reduce log level to `warn` in production
2. Enable log sampling for DEBUG level
3. Check for log spam in loops

## Migration Checklist

- [x] Install Zap logger dependency
- [x] Install New Relic agent dependency
- [x] Create logger package
- [x] Create tracing middleware
- [x] Create New Relic client wrapper
- [x] Update main.go with new logging
- [x] Update handlers with structured logging
- [x] Add data masking utilities
- [x] Add health check endpoints
- [ ] Update all use cases with logging
- [ ] Update all repositories with logging
- [ ] Configure New Relic dashboards
- [ ] Set up alerts
- [ ] Update deployment scripts

## Resources

- [Zap Logger Documentation](https://pkg.go.dev/go.uber.org/zap)
- [New Relic Go Agent](https://docs.newrelic.com/docs/apm/agents/go-agent/)
- [New Relic Query Language](https://docs.newrelic.com/docs/query-your-data/nrql-new-relic-query-language/)
- [Distributed Tracing Concept](https://docs.newrelic.com/docs/distributed-tracing/concepts/)
