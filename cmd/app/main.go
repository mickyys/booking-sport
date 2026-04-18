package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/infra"
	mg "github.com/hamp/booking-sport/internal/infra/mailgun"
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

	// 2. Crear índices de MongoDB
	if err := mongo.EnsureIndexes(ctx, db); err != nil {
		log.Printf("Warning: Error creando índices de MongoDB: %v", err)
	}

	// 3. Inicializar Repositorios
	sportCenterRepo := mongo.NewSportCenterRepository(db)
	if err := sportCenterRepo.SyncCourtsCount(ctx); err != nil {
		log.Printf("Warning: Error sincronizando contador de canchas: %v", err)
	}
	courtRepo := mongo.NewCourtRepository(db)
	userRepo := mongo.NewUserRepository(db)
	bookingRepo := mongo.NewBookingRepository(db)
	recurringReservationRepo := mongo.NewRecurringReservationRepository(db)

	// 4. Inicializar Casos de Uso (Application Layer)
	sportCenterUC := app.NewSportCenterUseCase(sportCenterRepo, courtRepo, userRepo, bookingRepo, recurringReservationRepo)
	// Inicializar Mailer (Mailgun) si está configurado
	var bookingMailer app.Mailer
	mailgunAPIKey := os.Getenv("MAILGUN_API_KEY")
	mailgunDomain := os.Getenv("MAILGUN_DOMAIN")
	mailgunFrom := os.Getenv("MAILGUN_FROM")
	mailgunTemplate := os.Getenv("MAILGUN_TEMPLATE_CONFIRMATION")
	mailgunTemplateCancel := os.Getenv("MAILGUN_TEMPLATE_CANCEL")

	if mailgunAPIKey != "" && mailgunDomain != "" && mailgunFrom != "" {
		mgMailer := mg.NewMailgunMailer(mailgunAPIKey, mailgunDomain, mailgunFrom, mailgunTemplate, mailgunTemplateCancel)
		bookingMailer = mgMailer
		log.Println("Mailgun mailer initialized")
	}

	bookingUC := app.NewBookingUseCase(bookingRepo, courtRepo, sportCenterRepo, userRepo, bookingMailer, recurringReservationRepo)

	courtUC := app.NewCourtUseCase(courtRepo, sportCenterRepo, bookingRepo, bookingUC)
	// 5. Inicializar Manejadores (Presentation Layer)
	sportCenterHandler := infra.NewSportCenterHandler(sportCenterUC)
	courtHandler := infra.NewCourtHandler(courtUC)
	bookingHandler := infra.NewBookingHandler(bookingUC)
	contactHandler := infra.NewContactHandler(bookingMailer)

	// 6. Configurar Rutas
	r := gin.Default()

	// Configurar CORS
	r.Use(cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			// Permitir localhost:5173, localhost:3000 y sus subdominios
			return origin == "http://localhost:5173" ||
				origin == "http://localhost:3000" ||
				strings.HasSuffix(origin, ".localhost:3000") ||
				strings.HasSuffix(origin, ".reservaloya.cl")
		},
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
	r.GET("/api/cities", sportCenterHandler.ListCities)
	r.GET("/api/sport-centers/slug/:slug", sportCenterHandler.GetBySlug)
	r.POST("/api/sport-centers", sportCenterHandler.Create)
	r.PUT("/api/sport-centers/:id", sportCenterHandler.Update)
	r.GET("/api/sport-centers/:id/schedules", sportCenterHandler.GetSchedules)
	r.GET("/api/courts", courtHandler.List)
	r.POST("/api/courts", courtHandler.CreateCourt)
	r.GET("/api/courts/:id/schedule", courtHandler.GetSchedule)
	r.POST("/api/bookings/fintoc", bookingHandler.CreateFintocPaymentIntent)
	r.POST("/api/bookings/fintoc/webhook", bookingHandler.FintocWebhook)
	r.GET("/api/bookings/fintoc/return", bookingHandler.FintocReturn)
	r.GET("/api/bookings/fintoc/:id", bookingHandler.GetFintocPaymentIntentStatus)
	// MercadoPago payment routes
	r.POST("/api/bookings/mercadopago", bookingHandler.CreateMercadoPagoPayment)
	r.POST("/api/bookings/mercadopago/webhook", bookingHandler.MercadoPagoWebhook)
	r.GET("/api/bookings/mercadopago/return", bookingHandler.MercadoPagoReturn)
	r.GET("/api/bookings/code/:code", bookingHandler.GetByBookingCode)
	r.POST("/api/bookings/code/:code/cancel", bookingHandler.CancelByBookingCode)
	r.POST("/api/bookings", bookingHandler.CreateBooking)
	r.POST("/api/contact", contactHandler.Submit)

	// Rutas Protegidas
	api := r.Group("/api")
	api.Use(authMiddleware)
	{
		// Endpoint seguro para obtener schedules con detalles de reservas
		api.GET("/sport-centers/:id/schedules/bookings", sportCenterHandler.GetSchedulesWithBookings)
		// Endpoint para administradores: obtener agenda automáticamente sin pasar id
		api.GET("/admin/sport-centers/schedules/bookings", sportCenterHandler.GetAdminSchedulesWithBookings)
		api.GET("/bookings/:id", bookingHandler.GetBookingDetail)
		api.GET("/bookings/my-bookings", bookingHandler.GetUserBookings)
		api.GET("/bookings/my-cancelled", bookingHandler.GetUserCancelledBookings)
		api.GET("/bookings/confirmed/count", bookingHandler.GetConfirmedCount)
		api.POST("/bookings/:id/cancel", bookingHandler.CancelBooking)
		api.GET("/admin/dashboard", bookingHandler.GetAdminDashboard)
		api.DELETE("/admin/bookings/series/:series_id", bookingHandler.DeleteBookingSeries)
		api.GET("/admin/bookings/series", bookingHandler.GetRecurringSeries)
		api.GET("/admin/courts", courtHandler.GetAdminCourts)
		api.POST("/admin/courts", courtHandler.CreateAdminCourt)
		api.PUT("/admin/courts/:id", courtHandler.UpdateAdminCourt)
		api.DELETE("/admin/courts/:id", courtHandler.DeleteAdminCourt)
		api.PUT("/admin/courts/:id/schedule", courtHandler.ConfigureSchedule)
		api.POST("/admin/courts/:id/schedule/preview", courtHandler.GetAffectedBookings)
		api.PATCH("/admin/courts/:id/schedule/slot", courtHandler.UpdateScheduleSlot)
		api.PUT("/admin/sport-centers/:id", sportCenterHandler.Update)
		api.PATCH("/admin/sport-centers/:id/settings", sportCenterHandler.UpdateSettings)
		api.GET("/admin/sport-centers/:id", sportCenterHandler.GetByID)
		api.POST("/admin/bookings/internal", bookingHandler.CreateInternalBooking)
		api.POST("/admin/bookings/:id/pay-balance", bookingHandler.MarkPartialPaymentAsPaid)
		api.PATCH("/admin/bookings/:id/undo-pay-balance", bookingHandler.UndoBalancePayment)
		api.DELETE("/admin/bookings/:id", bookingHandler.DeleteBooking)

		// Recurring Reservation routes
		api.POST("/admin/recurring", bookingHandler.CreateRecurringReservation)
		api.GET("/admin/recurring", bookingHandler.GetRecurringReservationsByCenter)
		api.GET("/admin/recurring/:id", bookingHandler.GetRecurringReservation)
		api.GET("/admin/recurring/court/:courtId", bookingHandler.GetRecurringReservationsByCourt)
		api.DELETE("/admin/recurring/:id", bookingHandler.CancelRecurringReservation)
	}

	// 7. Iniciar Servidor
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Servidor escuchando en puerto %s...\n", port)
	log.Fatal(r.Run(":" + port))
}
