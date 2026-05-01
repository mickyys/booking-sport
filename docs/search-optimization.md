# Optimización de Búsqueda por Nombre

## Problema Solucionado

La búsqueda por nombre en el frontend no encontraba resultados con coincidencias parciales. Por ejemplo, al escribir "cato" no aparecía "Centro Deportivo Católica" hasta escribir el nombre completo.

**Causa:** Se estaba usando `$text` search de MongoDB, que requiere palabras completas y no soporta búsqueda parcial desde el inicio.

## Solución Implementada

### 1. Cambio en el Repositorio (`internal/infra/mongo/repository.go`)

Se reemplazó la búsqueda `$text` por regex con anclaje de inicio (`^`):

```go
// Antes (no funcionaba con partial matching)
match["$text"] = bson.M{"$search": searchText}

// Ahora (funciona con partial matching desde el inicio)
bson.M{"$regex": "^" + regexp.QuoteMeta(name), "$options": "i"}
```

**Ventajas del regex anclado (`^pattern`):**
- MongoDB puede usar índices para búsquedas con prefijo fijo
- Búsquedas más rápidas que regex sin anclaje
- Case-insensitive con `$options: "i"`
- "cato" encuentra "Católica", "Centro Católico", etc.

### 2. Nuevo Índice (`internal/infra/mongo/indexes.go`)

Se agregó un índice en el campo `name`:

```go
{
    Keys:    bson.D{{Key: "name", Value: 1}},
    Options: options.Index().SetName("idx_sport_centers_name"),
}
```

## Cómo Aplicar los Cambios

### Opción 1: Usando el script de migración (Recomendado)

```bash
cd /Users/hectormartinez/hamp/reservaloya/booking-sport

# Asegúrate de tener las variables de entorno configuradas
export MONGO_URI="mongodb://localhost:27017"
export MONGO_DB="booking-sport"

# Ejecutar la migración
go run cmd/migrate-name-index/main.go
```

### Opción 2: Manualmente desde MongoDB Shell

```javascript
use booking-sport

// Crear índice en name
db.sport_centers.createIndex({ name: 1 }, { name: "idx_sport_centers_name" })

// Opcional: Eliminar índice de texto (ya no se usa para name)
db.sport_centers.dropIndex("idx_sport_centers_text")

// Ver índices actuales
db.sport_centers.getIndexes()
```

### Opción 3: Automáticamente al reiniciar la aplicación

El nuevo índice se creará automáticamente al iniciar la aplicación gracias a `EnsureIndexes()` en `internal/infra/mongo/indexes.go`.

## Comportamiento de la Búsqueda

### Ejemplos:

| Búsqueda | Encuentra | No encuentra |
|----------|-----------|--------------|
| `cato` | Católica, Centro Católico | Deportivo, Los Leones |
| `catolica` | Católica, Centro Deportivo Católica | Catedral, Centro |
| `centro` | Centro Deportivo, Centro Católico | Católica (si no empieza con "centro") |
| `deport` | Deportivo, Centro Deportivo | (ninguno si no empieza con "deport") |

### Notas Importantes:

1. **Búsqueda case-insensitive:** "cato", "Cato", "CATO" producen el mismo resultado
2. **Solo prefijos:** La búsqueda encuentra coincidencias que **comienzan** con el patrón
3. **Rendimiento:** El índice hace que las búsquedas sean O(log n) en lugar de O(n)

## Verificación

Para verificar que el índice se está usando:

```javascript
use booking-sport

// Explicar la consulta de búsqueda
db.sport_centers.explain("executionStats").find({
    name: { $regex: "^cato", $options: "i" }
})
```

Busca en el resultado:
- `executionStats.totalDocsExamined`: debería ser bajo (solo docs examinados)
- `executionStats.inputStage.indexName`: debería ser `idx_sport_centers_name`
- `executionStats.executionTimeMillis`: debería ser 0 o muy bajo

## Optimizaciones Futuras

Si se necesita búsqueda en cualquier parte del nombre (no solo prefijos):

1. **Wildcard Index (MongoDB 4.4+):**
   ```javascript
   db.sport_centers.createIndex({ name: "text" }, { weights: { name: 1 } })
   ```

2. **Búsqueda full-text con análisis de texto:**
   - Permite buscar "catolica" y encontrar "Centro Deportivo Católica"
   - Requiere índice de texto y configuración de idioma

3. **Search Atlas (si usa MongoDB Atlas):**
   - Búsqueda full-text avanzada
   - Relevancia, sinónimos, fuzzy matching

## Referencias

- [MongoDB Regex Performance](https://www.mongodb.com/docs/manual/reference/operator/query/regex/#performance-considerations)
- [MongoDB Indexing Strategies](https://www.mongodb.com/docs/manual/indexes/)
