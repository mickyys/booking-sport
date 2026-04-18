# Plantilla de Proyecto Go - ReservaLoYa

## Estructura del Proyecto

```
booking-sport/
├── cmd/app/main.go           # Punto de entrada, configuración de rutas
├── configs/                   # Archivos de configuración
├── internal/
│   ├── app/                   # Casos de uso / Lógica de negocio
│   │   ├── usecase.go         # Interfaces de repositorios
│   │   └── *_usecase.go       # Implementación de casos de uso
│   ├── domain/                # Entidades y tipos de dominio
│   │   └── entities.go
│   └── infra/                 # Capa de infraestructura
│       ├── handler.go         # Handlers HTTP (Gin)
│       ├── booking_handler.go
│       ├── *_handler.go
│       ├── mongo/             # Implementación MongoDB
│       │   ├── repository.go
│       │   └── *_repository.go
│       └── mailgun/           # Integraciones externas
├── pkg/auth/                  # Paquetes compartidos (auth, utils)
├── docs/                      # Documentación API
├── docker-compose.yml         # Servicios externos (MongoDB)
├── Dockerfile.dev/prod       # Docker
├── go.mod                     # Dependencias
└── .env                       # Variables de entorno
```

## Stack Tecnológico

| Componente | Tecnología | Versión |
|------------|------------|---------|
| Lenguaje | Go | 1.25+ |
| Framework HTTP | Gin | v1.12.0 |
| Base de datos | MongoDB | - |
| Driver DB | mongo-driver | v1.17.9 |
| Auth | JWT (Auth0) | v5.3.1 |
| CORS | gin-contrib/cors | v1.7.6 |
| Email | Mailgun | v4.11.0 |
| Pagos | MercadoPago, Fintoc | - |

## Patrón de Arquitectura (Clean Architecture)

### 1. Domain Layer (`internal/domain/`)
- Entidades puras sin dependencias externas
- Tipos, constantes, enumeraciones

```go
type Court struct {
    ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
    SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
    Name          string             `bson:"name" json:"name"`
    // ...
}
```

### 2. Application Layer (`internal/app/`)
- Casos de uso
- Interfaces de repositorios (contratos)
- Inyección de dependencias

```go
// Interfaz de repositorio (contrato)
type BookingRepository interface {
    Create(ctx context.Context, booking *domain.Booking) error
    FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Booking, error)
    // ...
}

// Caso de uso
type BookingUseCase struct {
    repo        BookingRepository
    courtRepo   CourtRepository
    centerRepo  SportCenterRepository
    mailer      Mailer
}
```

### 3. Infrastructure Layer (`internal/infra/`)

#### Handlers (Presentación)
```go
func (h *BookingHandler) CreateBooking(c *gin.Context) {
    // Parsear request
    // Llamar caso de uso
    // Responder
}
```

#### Repositorios (Persistencia)
```go
func (r *BookingRepository) Create(ctx context.Context, booking *domain.Booking) error {
    // Implementación MongoDB
}
```

## Convenciones de Código

### Nombres
- **Interfaces**: `NombreRepository`, `NombreService`, `NombreHandler`
- **Implementaciones**: `NewNombreRepository`, `NewNombreHandler`
- **Métodos**: `camelCase`
- **Variables exportadas**: `PascalCase`
- **Variables privadas**: `camelCase` o `snake_case`

### Estructura de Respuestas API

```go
// Éxito
c.JSON(http.StatusOK, gin.H{"data": result})

// Error
c.JSON(http.StatusBadRequest, gin.H{"error": "mensaje de error"})
```

### Middleware de Autenticación

```go
authMiddleware := auth.EnsureValidToken(
    os.Getenv("AUTH0_DOMAIN"),
    os.Getenv("AUTH0_AUDIENCE"),
)

api := r.Group("/api")
api.Use(authMiddleware)
```

### Manejo de Errores

```go
// En handler
if err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
    return
}

// En use case
func (uc *BookingUseCase) Create(...) error {
    if booking == nil {
        return fmt.Errorf("booking no puede ser nil")
    }
    return uc.repo.Create(ctx, booking)
}
```

## Configuración

### Variables de Entorno Requeridas

```bash
# Base de datos
MONGO_URI=mongodb://localhost:27017

# Auth0
AUTH0_DOMAIN=tu-dominio.auth0.com
AUTH0_AUDIENCE=https://tu-api.com

# Email
MAILGUN_API_KEY=xxx
MAILGUN_DOMAIN=mg.tudominio.com
MAILGUN_FROM=noreply@tudominio.com

# Email templates
MAILGUN_TEMPLATE_CONFIRMATION=booking_confirmation
MAILGUN_TEMPLATE_CANCEL=booking_cancellation

# Servidor
PORT=8080
```

### CORS Configuration

```go
r.Use(cors.New(cors.Config{
    AllowOriginFunc: func(origin string) bool {
        return origin == "http://localhost:3000" ||
            strings.HasSuffix(origin, ".tudominio.com")
    },
    AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
    AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
    AllowCredentials: true,
    MaxAge:           12 * time.Hour,
}))
```

## Inicialización del Servidor

```go
func main() {
    // 1. Logger setup
    logFile, _ := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
    multiWriter := io.MultiWriter(os.Stdout, logFile)
    log.SetOutput(multiWriter)

    // 2. Conexión MongoDB
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
    defer client.Disconnect(ctx)

    // 3. Crear índices
    mongo.EnsureIndexes(ctx, db)

    // 4. Inicializar repositorios
    bookingRepo := mongo.NewBookingRepository(db)

    // 5. Inicializar casos de uso
    bookingUC := app.NewBookingUseCase(bookingRepo)

    // 6. Inicializar handlers
    bookingHandler := infra.NewBookingHandler(bookingUC)

    // 7. Configurar rutas
    r := gin.Default()
    // ... rutas ...

    // 8. Iniciar servidor
    log.Fatal(r.Run(":" + port))
}
```

## Patrones Comunes

### Repository Pattern
```go
// Interfaz (application layer)
type CourtRepository interface {
    Create(ctx context.Context, court *domain.Court) error
    FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Court, error)
    Update(ctx context.Context, court *domain.Court) error
    Delete(ctx context.Context, id primitive.ObjectID) error
    FindByCenterID(ctx context.Context, centerID primitive.ObjectID) ([]domain.Court, error)
}

// Implementación (infrastructure layer)
type CourtRepository struct {
    collection *mongo.Collection
}

func NewCourtRepository(db *mongo.Database) *CourtRepository {
    return &CourtRepository{
        collection: db.Collection("courts"),
    }
}

func (r *CourtRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.Court, error) {
    var court domain.Court
    err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&court)
    if err != nil {
        return nil, err
    }
    return &court, nil
}
```

### Handler Pattern
```go
func (h *CourtHandler) CreateCourt(c *gin.Context) {
    var req struct {
        SportCenterID string `json:"sport_center_id" binding:"required"`
        Name          string `json:"name" binding:"required"`
        Description   string `json:"description"`
    }

    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    centerID, err := primitive.ObjectIDFromHex(req.SportCenterID)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid center id"})
        return
    }

    court, err := h.useCase.CreateCourt(c.Request.Context(), centerID, req.Name, req.Description)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusCreated, gin.H{"data": court})
}
```

### Use Case Pattern
```go
func (uc *CourtUseCase) CreateCourt(ctx context.Context, centerID primitive.ObjectID, name, description string) (*domain.Court, error) {
    // Validaciones
    if name == "" {
        return nil, fmt.Errorf("name is required")
    }

    // Verificar que el centro existe
    center, err := uc.centerRepo.FindByID(ctx, centerID)
    if err != nil {
        return nil, fmt.Errorf("center not found: %w", err)
    }

    // Crear entidad
    court := &domain.Court{
        ID:            primitive.NewObjectID(),
        SportCenterID: centerID,
        Name:          name,
        Description:   description,
        CreatedAt:     time.Now(),
        UpdatedAt:     time.Now(),
    }

    // Persistir
    if err := uc.repo.Create(ctx, court); err != nil {
        return nil, err
    }

    return court, nil
}
```

## Tips para Nuevos Proyectos

### 1. Estructura Inicial
```bash
mkdir mi-proyecto
cd mi-proyecto
go mod init github.com/usuario/mi-proyecto
mkdir -p cmd/app internal/{app,domain,infra/{mongo,handlers}} pkg
```

### 2. Agregar Dependencias
```bash
go get github.com/gin-gonic/gin
go get go.mongodb.org/mongo-driver
go get github.com/gin-contrib/cors
```

### 3. Run para Verificar
```bash
go build ./...
go vet ./...
```

### 4. Desarrollo con Hot Reload
```bash
# Instalar air
go install github.com/cosmtrek/air@latest

# Usar .air.toml personalizado
air
```

### 5. Testing
```bash
go test ./...
go test -v -cover ./internal/app/
```

## Checklist de Nuevos Endpoints

- [ ] Definir entidad en `domain/entities.go`
- [ ] Agregar método a interfaz en `app/usecase.go`
- [ ] Implementar en `infra/mongo/*_repository.go`
- [ ] Implementar caso de uso en `app/*_usecase.go`
- [ ] Crear handler en `infra/*_handler.go`
- [ ] Registrar ruta en `cmd/app/main.go`
- [ ] Agregar tests
- [ ] Verificar con `go build ./...`