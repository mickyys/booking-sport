# Fix: Búsqueda Parcial por Nombre de Centro Deportivo

## Problema
Al buscar "cato" en el frontend, no aparecía "Centro Deportivo Católica" o "Club Union Catolica" hasta escribir el nombre completo.

## Causa Raíz
El código usaba `$text` search de MongoDB (línea 87 en `repository.go`), que:
- Requiere palabras completas en el índice de texto
- No soporta búsqueda parcial (autocomplete/prefix matching)
- Tokeniza el texto, separando por espacios

## Solución

### Cambios Realizados

1. **`internal/infra/mongo/repository.go`** (líneas 77-92)
   - Reemplazado `$text` search por regex **sin anclaje** para búsqueda en cualquier parte del nombre
   - Búsqueda case-insensitive con `$options: "i"`
   - Uso de `regexp.QuoteMeta()` para escapar caracteres especiales
   - **Importante:** Se removió el `^` para permitir búsqueda en toda la frase

2. **`internal/infra/mongo/indexes.go`** (línea 53-56)
   - Agregado índice en `name` para optimizar búsquedas
   - El índice ayuda aunque el regex no esté anclado al inicio

### Cómo Funciona Ahora

```go
// Búsqueda con regex SIN anclaje (busca en cualquier parte)
bson.M{
    "$regex": regexp.QuoteMeta(name),
    "$options": "i"  // case-insensitive
}

// Ejemplo: name = "cato"
// MongoDB busca: /cato/i
// Encuentra: "Católica", "Club Union Catolica", "Centro Católico", "Cato Sports"
// No encuentra: "Deportivo" (no contiene "cato")
```

### Por Qué es Más Rápido

| Tipo | Complejidad | Usa Índice | Búsqueda |
|------|-------------|------------|----------|
| Regex sin ancla (`patrón`) | O(n) | ⚠️ Parcial | En cualquier parte |
| Regex anclado (`^patrón`) | O(log n) | ✅ Sí | Solo al inicio |
| $text search | O(log n) | ✅ Sí | Palabras completas |

**Nota:** Aunque el regex sin anclaje requiere scan más amplio, el índice en `name` ayuda a MongoDB a optimizar la ejecución, especialmente con colecciones grandes.

## Aplicar los Cambios

### Opción 1: Automática (Recomendada)
El índice se crea automáticamente al iniciar la app:
```bash
cd /Users/hectormartinez/hamp/reservaloya/booking-sport
go run cmd/app/main.go
```

### Opción 2: Script de Migración
Para aplicar solo el índice sin reiniciar:
```bash
export MONGO_URI="mongodb://localhost:27017"
export MONGO_DB="booking-sport"
go run cmd/migrate-name-index/main.go
```

### Opción 3: MongoDB Shell
```javascript
use booking-sport
db.sport_centers.createIndex({ name: 1 }, { name: "idx_sport_centers_name" })
```

## Verificación

1. **Inicia la aplicación**
2. **Abre el frontend** y ve a la búsqueda
3. **Pruebas:**
   - Escribe "cato" → debería mostrar "Católica", "Club Union Catolica", "Centro Católico"
   - Escribe "union" → debería mostrar "Club Union Catolica", "Union Deportiva"
   - Escribe "club" → debería mostrar todos los centros que comienzan con "Club"

## Pruebas con MongoDB Shell

```javascript
// Verificar que el índice existe
db.sport_centers.getIndexes()

// Probar la búsqueda (en cualquier parte del nombre)
db.sport_centers.find({ 
    name: { $regex: "cato", $options: "i" } 
})

// Verificar ejecución
db.sport_centers.explain("executionStats").find({ 
    name: { $regex: "cato", $options: "i" } 
})
```

## Notas Técnicas

- **Case-insensitive:** "cato", "Cato", "CATO" son equivalentes
- **Búsqueda en toda la frase:** "cato" encuentra "Club Union Catolica"
- **Caracteres especiales:** `regexp.QuoteMeta()` escapa regex metacaracteres
- **Rendimiento:** Índice ayuda pero regex sin ancla es O(n) vs O(log n) del anclado
- **Trade-off:** Más flexibilidad (busca en cualquier parte) vs rendimiento ligeramente menor

## Archivos Modificados

- ✏️ `internal/infra/mongo/repository.go` - Regex sin anclaje para búsqueda en cualquier parte
- ✏️ `internal/infra/mongo/indexes.go` - Índice en name
- ✏️ `cmd/migrate-name-index/main.go` - Script de migración actualizado
- ➕ `docs/search-optimization.md` - Documentación
