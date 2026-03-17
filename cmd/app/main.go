package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/infra"
	"github.com/hamp/booking-sport/internal/infra/mongo"
	"github.com/hamp/booking-sport/pkg/auth"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	// Logger setup for local development
	logFile, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		// Output to both stdout and file
		multiWriter := io.MultiWriter(os.Stdout, logFile)
		log.SetOutput(multiWriter)
		// Configure Gin to also write to both
		gin.DefaultWriter = multiWriter
		gin.DefaultErrorWriter = multiWriter

		log.Println("--- Application Start ---")
		log.Printf("Logging to app.log and stdout\n")
		// No usar defer logFile.Close() aquí si el proceso es de larga duración y queremos asegurar el flush
	} else {
		fmt.Printf("Error opening log file: %v\n", err)
	}

	// 1. Configurar conexión a MongoDB (usando env o valor por defecto)
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("Error al conectar a MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	db := client.Database("sport_booking")

	// 2. Inicializar Repositorios
	sportCenterRepo := mongo.NewSportCenterRepository(db)
	courtRepo := mongo.NewCourtRepository(db)
	userRepo := mongo.NewUserRepository(db)
	bookingRepo := mongo.NewBookingRepository(db)

	// 3. Inicializar Casos de Uso (Application Layer)
	sportCenterUC := app.NewSportCenterUseCase(sportCenterRepo, courtRepo, userRepo, bookingRepo)
	courtUC := app.NewCourtUseCase(courtRepo, sportCenterRepo, bookingRepo)
	bookingUC := app.NewBookingUseCase(bookingRepo, courtRepo, sportCenterRepo, userRepo)

	// 4. Inicializar Manejadores (Presentation Layer)
	sportCenterHandler := infra.NewSportCenterHandler(sportCenterUC)
	courtHandler := infra.NewCourtHandler(courtUC)
	bookingHandler := infra.NewBookingHandler(bookingUC)

	// 5. Configurar Rutas
	r := gin.Default()

	// Configurar CORS
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://localhost:3000"}, // Añadir orígenes permitidos
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Auth0 Middleware
	authMiddleware := auth.EnsureValidToken(
		os.Getenv("AUTH0_DOMAIN"),
		os.Getenv("AUTH0_AUDIENCE"),
	)

	// Rutas Públicas
	r.GET("/api/sport-centers", sportCenterHandler.List)
	r.POST("/api/sport-centers", sportCenterHandler.Create)
	r.PUT("/api/sport-centers/:id", sportCenterHandler.Update)
	r.GET("/api/sport-centers/:id/schedules", sportCenterHandler.GetSchedules)
	r.GET("/api/courts", courtHandler.List)
	r.POST("/api/courts", courtHandler.CreateCourt)
	r.PUT("/api/courts/:id/schedule", courtHandler.ConfigureSchedule)
	r.GET("/api/courts/:id/schedule", courtHandler.GetSchedule)
	r.POST("/api/bookings/fintoc", bookingHandler.CreateFintocPaymentIntent)
	r.POST("/api/bookings/fintoc/webhook", bookingHandler.FintocWebhook)
	r.GET("/api/bookings/fintoc/return", bookingHandler.FintocReturn)
	r.GET("/api/bookings/fintoc/:id", bookingHandler.GetFintocPaymentIntentStatus)
	r.GET("/api/bookings/code/:code", bookingHandler.GetByBookingCode)

	// Rutas Protegidas
	api := r.Group("/api")
	api.Use(authMiddleware)
	{
		api.GET("/bookings/my-bookings", bookingHandler.GetUserBookings)
		api.GET("/bookings/confirmed/count", bookingHandler.GetConfirmedCount)
		api.POST("/bookings/:id/cancel", bookingHandler.CancelBooking)
	}

	// 6. Iniciar Servidor
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Servidor escuchando en puerto %s...\n", port)
	log.Fatal(r.Run(":" + port))
}
