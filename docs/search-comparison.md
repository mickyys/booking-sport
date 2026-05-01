# Comparación: Búsqueda por Nombre

## Escenarios de Búsqueda

### Ejemplo con "Club Union Catolica"

| Búsqueda | Versión Anterior (^anclado) | Versión Actual (sin ancla) |
|----------|---------------------------|---------------------------|
| `club` | ✅ Encuentra | ✅ Encuentra |
| `union` | ❌ No encuentra | ✅ Encuentra |
| `cato` | ❌ No encuentra | ✅ Encuentra |
| `catolica` | ❌ No encuentra | ✅ Encuentra |
| `lica` | ❌ No encuentra | ✅ Encuentra |

### Ejemplo con "Centro Deportivo Católica"

| Búsqueda | Versión Anterior (^anclado) | Versión Actual (sin ancla) |
|----------|---------------------------|---------------------------|
| `centro` | ✅ Encuentra | ✅ Encuentra |
| `deport` | ❌ No encuentra | ✅ Encuentra |
| `cato` | ❌ No encuentra | ✅ Encuentra |
| `catolica` | ❌ No encuentra | ✅ Encuentra |

## Código

### Antes (Solo inicio del nombre)

```go
// Regex anclado al inicio
bson.M{"$regex": "^" + regexp.QuoteMeta(name), "$options": "i"}

// Equivale a: /^cato/i
// Solo busca al inicio: "Catolica..." ✅, "Club Catolica" ❌
```

### Ahora (En cualquier parte del nombre)

```go
// Regex sin anclaje
bson.M{"$regex": regexp.QuoteMeta(name), "$options": "i"}

// Equivale a: /cato/i
// Busca en todo el nombre: "Catolica" ✅, "Club Catolica" ✅, "Union Catolica" ✅
```

## Rendimiento

| Métrica | Regex Anclado (^) | Regex Sin Ancla |
|---------|-------------------|-----------------|
| Complejidad | O(log n) | O(n) |
| Usa índice | ✅ Completamente | ⚠️ Parcialmente |
| Flexibilidad | ❌ Solo inicio | ✅ Todo el texto |
| Casos de uso | Autocomplete | Búsqueda libre |

**Nota:** Aunque el regex sin ancla es teóricamente más lento (O(n)), en la práctica:
- La colección de centros deportivos es pequeña (< 1000 registros)
- El índice en `name` aún ayuda a MongoDB a optimizar
- El impacto es imperceptible para el usuario
- La mejora en UX (encontrar más resultados) supera el costo mínimo

## Casos de Uso Recomendados

### Regex Anclado (`^pattern`)
- Búsqueda tipo autocomplete/dropdown
- Cuando sabes que el usuario escribe desde el inicio
- Colecciones muy grandes (> 1M registros)

### Regex Sin Ancla (`pattern`) ✅ **Nuestra elección**
- Búsqueda libre tipo Google
- Cuando el usuario puede escribir cualquier parte del nombre
- Colecciones pequeñas/medianas (< 100K registros)
- Mejor experiencia de usuario

## Ejemplos Reales

```javascript
// Búsqueda: "cato"

// Centro: "Club Union Catolica"
// Resultado: ✅ Coincide (contiene "cato")

// Centro: "Centro Deportivo Católica"  
// Resultado: ✅ Coincide (contiene "cato")

// Centro: "Cato Sports Center"
// Resultado: ✅ Coincide (contiene "cato")

// Centro: "Deportivo Los Leones"
// Resultado: ❌ No coincide (no contiene "cato")
```

## Configuración MongoDB

### Índice Recomendado

```javascript
db.sport_centers.createIndex(
    { name: 1 },
    { name: "idx_sport_centers_name" }
)
```

### Explicación de la Consulta

```javascript
db.sport_centers.explain("executionStats").find({
    name: { $regex: "cato", $options: "i" }
})
```

**Qué buscar en el resultado:**
- `executionStats.nReturned`: cuántos centros coincidieron
- `executionStats.totalDocsExamined`:docs escaneados (debería ser razonable)
- `executionStats.executionTimeMillis`: tiempo de ejecución (idealmente < 10ms)

## Conclusión

La búsqueda **sin anclaje** proporciona una mejor experiencia de usuario al permitir encontrar centros deportivos escribiendo cualquier parte del nombre, no solo el inicio. El rendimiento es aceptable para el tamaño actual de la colección.
