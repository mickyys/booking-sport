package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/infra"
	mg "github.com/hamp/booking-sport/internal/infra/mailgun"
	"github.com/hamp/booking-sport/internal/infra/middleware"
	"github.com/hamp/booking-sport/internal/infra/mongo"
	"github.com/hamp/booking-sport/pkg/auth"
	"github.com/hamp/booking-sport/pkg/logger"
	nr "github.com/hamp/booking-sport/pkg/newrelic"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	logConfig := logger.Config{
		Level:       getEnv("LOG_LEVEL", "info"),
		Format:      getEnv("LOG_FORMAT", "json"),
		Service:     "booking-sport-api",
		Version:     getEnv("SERVICE_VERSION", "1.0.0"),
		Environment: getEnv("ENVIRONMENT", "development"),
	}
	log := logger.Init(logConfig)

	nrConfig := nr.Config{
		LicenseKey:  nr.GetLicenseKeyFromEnv(),
		AppName:     nr.GetAppNameFromEnv(),
		Enabled:     nr.GetEnabledFromEnv(),
		Environment: logConfig.Environment,
	}
	_, err := nr.Init(nrConfig)
	if err != nil {
		log.Warnw("new_relic initialization failed", "error", err)
	}

	log.Infow("application_starting",
		"service", logConfig.Service,
		"version", logConfig.Version,
		"environment", logConfig.Environment,
		"log_level", logConfig.Level,
		"log_format", logConfig.Format,
	)

	mongoURI := getEnv("MONGO_URI", "mongodb://localhost:27017")

	log.Infow("connecting_to_mongodb",
		"mongo_uri", maskMongoURI(mongoURI),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalw("mongodb_connection_failed", "error", err)
	}
	defer client.Disconnect(ctx)

	log.Infow("mongodb_connected")

	db := client.Database("sport_booking")

	if err := mongo.EnsureIndexes(ctx, db); err != nil {
		log.Warnw("mongodb_indexes_creation_failed", "error", err)
	} else {
		log.Infow("mongodb_indexes_created")
	}

	sportCenterRepo := mongo.NewSportCenterRepository(db)
	if err := sportCenterRepo.SyncCourtsCount(ctx); err != nil {
		log.Warnw("sport_centers_count_sync_failed", "error", err)
	}
	courtRepo := mongo.NewCourtRepository(db)
	userRepo := mongo.NewUserRepository(db)
	bookingRepo := mongo.NewBookingRepository(db)
	recurringReservationRepo := mongo.NewRecurringReservationRepository(db)
	userDeviceRepo := mongo.NewUserDeviceRepository(db)

	log.Infow("repositories_initialized")

	sportCenterUC := app.NewSportCenterUseCase(sportCenterRepo, courtRepo, userRepo, bookingRepo, recurringReservationRepo)
	courtUC := app.NewCourtUseCase(courtRepo, sportCenterRepo, bookingRepo)

	var notifier app.NotificationService
	firebaseCredentialsFile := os.Getenv("FIREBASE_CREDENTIALS_FILE")
	if firebaseCredentialsFile != "" {
		fcmService, err := infra.NewFirebaseNotificationService(context.Background(), firebaseCredentialsFile)
		if err != nil {
			log.Warnw("firebase_initialization_failed", "error", err)
		} else {
			notifier = fcmService
			log.Infow("firebase_initialized")
		}
	} else {
		log.Infow("firebase_disabled", "reason", "credentials_not_configured")
	}

	var bookingMailer app.Mailer
	mailgunAPIKey := os.Getenv("MAILGUN_API_KEY")
	mailgunDomain := os.Getenv("MAILGUN_DOMAIN")
	mailgunFrom := os.Getenv("MAILGUN_FROM")
	mailgunTemplate := os.Getenv("MAILGUN_TEMPLATE_CONFIRMATION")
	mailgunTemplatePaid := os.Getenv("MAILGUN_TEMPLATE_PAID")
	mailgunTemplateCancel := os.Getenv("MAILGUN_TEMPLATE_CANCEL")

	if mailgunAPIKey != "" && mailgunDomain != "" && mailgunFrom != "" {
		mgMailer := mg.NewMailgunMailer(mailgunAPIKey, mailgunDomain, mailgunFrom, mailgunTemplate, mailgunTemplatePaid, mailgunTemplateCancel)
		bookingMailer = mgMailer
		log.Infow("mailgun_initialized",
			"domain", mailgunDomain,
			"from", logger.MaskEmail(mailgunFrom),
		)
	} else {
		log.Warnw("mailgun_not_configured")
	}

	bookingUC := app.NewBookingUseCase(bookingRepo, courtRepo, sportCenterRepo, userRepo, userDeviceRepo, bookingMailer, notifier, recurringReservationRepo)

	log.Infow("use_cases_initialized")

	sportCenterHandler := infra.NewSportCenterHandler(sportCenterUC)
	courtHandler := infra.NewCourtHandler(courtUC)
	bookingHandler := infra.NewBookingHandler(bookingUC)
	contactHandler := infra.NewContactHandler(bookingMailer)

	log.Infow("handlers_initialized")

	ginMode := getEnv("GIN_MODE", "release")
	gin.SetMode(ginMode)
	r := gin.New()

	r.Use(middleware.Recovery())
	r.Use(middleware.Tracing())
	r.Use(middleware.NewRelicMiddleware())

	r.Use(cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			return origin == "http://localhost:5173" ||
				origin == "http://localhost:3000" ||
				strings.HasSuffix(origin, ".localhost:3000") ||
				strings.HasSuffix(origin, ".reservaloya.cl")
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Trace-ID"},
		ExposeHeaders:    []string{"Content-Length", "X-Trace-ID"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	log.Infow("cors_configured")

	authMiddleware := auth.EnsureValidToken(
		os.Getenv("AUTH0_DOMAIN"),
		os.Getenv("AUTH0_AUDIENCE"),
	)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":    "healthy",
			"service":   "booking-sport-api",
			"version":   logConfig.Version,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	})

	r.GET("/ready", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "ready",
			"service": "booking-sport-api",
		})
	})

	log.Infow("routes_configured")

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
		// Registro de dispositivos para notificaciones
		api.POST("/users/devices", bookingHandler.RegisterDevice)

		// Endpoint seguro para obtener schedules con detalles de reservas
		api.GET("/sport-centers/:id/schedules/bookings", sportCenterHandler.GetSchedulesWithBookings)
		// Endpoint para administradores: obtener agenda automáticamente sin pasar id
		api.GET("/admin/my-sport-center", sportCenterHandler.GetMySportCenter)
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
		api.PATCH("/admin/courts/:id/schedule/slot", courtHandler.UpdateScheduleSlot)
		api.PUT("/admin/sport-centers/:id", sportCenterHandler.Update)
		api.PATCH("/admin/sport-centers/:id/settings", sportCenterHandler.UpdateSportCenterSettings)
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

		// Users management routes
		api.GET("/admin/users", sportCenterHandler.GetCenterUsers)
		api.DELETE("/admin/users/:userId", sportCenterHandler.RemoveCenterUser)
	}

	port := getEnv("PORT", "8080")

	log.Infow("server_starting",
		"port", port,
		"gin_mode", ginMode,
	)

	fmt.Printf("Servidor escuchando en puerto %s...\n", port)
	
	if err := r.Run(":" + port); err != nil {
		log.Fatalw("server_startup_failed", "error", err, "port", port)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func maskMongoURI(uri string) string {
	if uri == "" {
		return ""
	}
	if strings.Contains(uri, "@") {
		parts := strings.SplitN(uri, "@", 2)
		if len(parts) == 2 {
			return "***@" + parts[1]
		}
	}
	return "***"
}
