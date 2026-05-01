# Fix: Búsqueda Parcial por Nombre de Centro Deportivo

## Problema
Al buscar "cato" en el frontend, no aparecía "Centro Deportivo Católica" hasta escribir el nombre completo.

## Causa Raíz
El código usaba `$text` search de MongoDB (línea 87 en `repository.go`), que:
- Requiere palabras completas en el índice de texto
- No soporta búsqueda parcial (autocomplete/prefix matching)
- Tokeniza el texto, separando por espacios

## Solución

### Cambios Realizados

1. **`internal/infra/mongo/repository.go`** (líneas 77-92)
   - Reemplazado `$text` search por regex con anclaje de inicio
   - Búsqueda case-insensitive con `$options: "i"`
   - Uso de `regexp.QuoteMeta()` para escapar caracteres especiales

2. **`internal/infra/mongo/indexes.go`** (línea 53-56)
   - Agregado índice en `name` para optimizar búsquedas con regex
   - El índice permite que MongoDB use index scan en lugar de collection scan

3. **`cmd/migrate-name-index/main.go`** (nuevo archivo)
   - Script para aplicar el índice en la base de datos existente
   - Opcional: elimina el índice de texto que ya no se necesita

4. **`docs/search-optimization.md`** (nuevo archivo)
   - Documentación completa de la optimización

### Cómo Funciona Ahora

```go
// Búsqueda con regex anclado
bson.M{
    "$regex": "^" + regexp.QuoteMeta(name),
    "$options": "i"  // case-insensitive
}

// Ejemplo: name = "cato"
// MongoDB busca: /^cato/i
// Encuentra: "Católica", "Centro Católico", "Cato Sports"
// No encuentra: "Deportivo Cato" (no empieza con "cato")
```

### Por Qué es Más Rápido

| Tipo | Complejidad | Usa Índice |
|------|-------------|------------|
| Regex anclado (`^patrón`) | O(log n) | ✅ Sí |
| Regex sin ancla (`patrón`) | O(n) | ❌ No |
| $text search | O(log n) | ✅ Sí (pero sin partial match) |

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
3. **Escribe "cato"** → debería mostrar centros con nombres que comienzan con "cato"
4. **Escribe "centro"** → debería mostrar centros que comienzan con "centro"

## Pruebas con MongoDB Shell

```javascript
// Verificar que el índice existe
db.sport_centers.getIndexes()

// Probar la búsqueda
db.sport_centers.find({ 
    name: { $regex: "^cato", $options: "i" } 
})

// Verificar que usa el índice
db.sport_centers.explain("executionStats").find({ 
    name: { $regex: "^cato", $options: "i" } 
})
```

## Notas Técnicas

- **Case-insensitive:** "cato", "Cato", "CATO" son equivalentes
- **Solo prefijos:** Busca coincidencias que comienzan con el patrón
- **Caracteres especiales:** `regexp.QuoteMeta()` escapa regex metacaracteres
- **Rendimiento:** Índice reduce búsquedas de O(n) a O(log n)

## Archivos Modificados

- ✏️ `internal/infra/mongo/repository.go`
- ✏️ `internal/infra/mongo/indexes.go`
- ➕ `cmd/migrate-name-index/main.go` (nuevo)
- ➕ `docs/search-optimization.md` (nuevo)
