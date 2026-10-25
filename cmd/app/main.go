package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
	"github.com/hamp/booking-sport/internal/infra"
	"github.com/hamp/booking-sport/internal/infra/auth0mgmt"
	mg "github.com/hamp/booking-sport/internal/infra/mailgun"
	"github.com/hamp/booking-sport/internal/infra/middleware"
	"github.com/hamp/booking-sport/internal/infra/mongo"
	occredis "github.com/hamp/booking-sport/internal/infra/redis"
	"github.com/hamp/booking-sport/pkg/auth"
	"github.com/hamp/booking-sport/pkg/logger"
	"github.com/hamp/booking-sport/pkg/mercadopago"
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
	nrApp, err := nr.Init(nrConfig)
	if err != nil {
		log.Warnw("new_relic initialization failed", "error", err)
	}

	if nrApp != nil {
		log = logger.ReconfigureForNewRelic(logConfig, nrApp)
		log.Infow("new_relic_logs_enabled")
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
	holdRepo := mongo.NewSlotHoldRepository(db)
	recurringReservationRepo := mongo.NewRecurringReservationRepository(db)
	jobLockRepo := mongo.NewJobLockRepository(db)
	userDeviceRepo := mongo.NewUserDeviceRepository(db)
	facilityRepo := mongo.NewFacilityRepository(db)
	spaceRepo := mongo.NewSpaceRepository(db)
	scheduleRuleRepo := mongo.NewScheduleRuleRepository(db)
	scheduleExceptionRepo := mongo.NewScheduleExceptionRepository(db)
	blockRepo := mongo.NewBlockRepository(db)
	userProfileRepo := mongo.NewUserProfileRepository(db)

	log.Infow("repositories_initialized")

	mpConnectClient := mercadopago.NewConnectClient(
		getEnv("NEXT_PUBLIC_MP_CLIENT_ID", ""),
		getEnv("MP_CLIENT_SECRET", ""),
		getEnv("MP_REDIRECT_URI", ""),
		getEnv("MP_AUTH_BASE", ""),
		getEnv("NEXT_PUBLIC_MP_PUBLIC_KEY", ""),
	)
	mpConnectUC := app.NewMPConnectUseCase(sportCenterRepo, mpConnectClient, getEnv("MP_CLIENT_SECRET", ""))

	sportCenterUC := app.NewSportCenterUseCase(sportCenterRepo, courtRepo, userRepo, bookingRepo, holdRepo, recurringReservationRepo, spaceRepo, scheduleRuleRepo)
	sportCenterUC.SetMPDisconnector(mpConnectUC)
	courtUC := app.NewCourtUseCase(courtRepo, sportCenterRepo, bookingRepo)
	facilityUC := app.NewFacilityUseCase(facilityRepo, sportCenterRepo)
	spaceUC := app.NewSpaceUseCase(spaceRepo, sportCenterRepo)
	scheduleUC := app.NewScheduleUseCase(scheduleRuleRepo, scheduleExceptionRepo, blockRepo, spaceRepo, sportCenterRepo)
	legacyFacilityBookingRepo := mongo.NewFacilityBookingRepository(db)
	sportCenterUC.SetScheduleResolver(app.NewScheduleResolver(scheduleRuleRepo, scheduleExceptionRepo, blockRepo))

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
	var facilityMailer app.FacilityMailer
	var memberVerificationMailer app.MemberVerificationMailer
	mailgunAPIKey := os.Getenv("MAILGUN_API_KEY")
	mailgunDomain := os.Getenv("MAILGUN_DOMAIN")
	mailgunFrom := os.Getenv("MAILGUN_FROM")
	mailgunTemplate := os.Getenv("MAILGUN_TEMPLATE_CONFIRMATION")
	mailgunTemplatePaid := os.Getenv("MAILGUN_TEMPLATE_PAID")
	mailgunTemplateCancel := os.Getenv("MAILGUN_TEMPLATE_CANCEL")
	mailgunTemplateWelcomeCenter := os.Getenv("MAILGUN_TEMPLATE_WELCOME_CENTER")

	if mailgunAPIKey != "" && mailgunDomain != "" && mailgunFrom != "" {
		mgMailer := mg.NewMailgunMailer(mailgunAPIKey, mailgunDomain, mailgunFrom, mailgunTemplate, mailgunTemplatePaid, mailgunTemplateCancel, mailgunTemplateWelcomeCenter)
		mgMailer.SetFacilityTemplate(os.Getenv("MAILGUN_TEMPLATE_FACILITY"))
		bookingMailer = mgMailer
		facilityMailer = mgMailer
		memberVerificationMailer = mgMailer
		log.Infow("mailgun_initialized",
			"domain", mailgunDomain,
			"from", logger.MaskEmail(mailgunFrom),
		)
	} else {
		log.Warnw("mailgun_not_configured")
	}

	bookingUC := app.NewBookingUseCase(bookingRepo, holdRepo, courtRepo, sportCenterRepo, userRepo, userDeviceRepo, bookingMailer, notifier, recurringReservationRepo, client, mercadopago.NewClient(getEnv("MP_ACCESS_TOKEN", "")))
	bookingResolver := app.NewScheduleResolver(scheduleRuleRepo, scheduleExceptionRepo, blockRepo)
	bookingUC.SetScheduleResolver(bookingResolver)
	bookingUC.SetJobLockRepository(jobLockRepo)
	if getEnv("MP_RECONCILE_ENABLED", "true") != "false" {
		mpInterval, err := time.ParseDuration(getEnv("MP_RECONCILE_INTERVAL", "3m"))
		if err != nil || mpInterval <= 0 {
			mpInterval = 3 * time.Minute
		}
		go bookingUC.RunPaymentReconcileLoop(context.Background(), jobLockRepo, mpInterval)
	}

	// Socios y códigos de descuento
	membershipRepo := mongo.NewMembershipRepository(db)
	discountRepo := mongo.NewDiscountRepository(db)
	membershipUC := app.NewMembershipUseCase(membershipRepo, sportCenterRepo)
	discountUC := app.NewDiscountUseCase(discountRepo, sportCenterRepo)
	memberVerificationUC := app.NewMemberVerificationService(membershipRepo, sportCenterRepo, memberVerificationMailer)
	pricingService := app.NewPricingService(courtRepo, sportCenterRepo, spaceRepo, bookingResolver, membershipRepo, discountRepo)
	bookingUC.SetPricingService(pricingService)

	if getEnv("BOOKING_REDIS_AUTHORITY", "false") == "true" {
		maxDuration, err := time.ParseDuration(getEnv("BOOKING_MAX_DURATION", "24h"))
		if err != nil {
			log.Fatalw("invalid_booking_max_duration", "error", err)
		}
		coordinator, err := occredis.NewCoordinator(occredis.Config{
			URL:         getEnv("REDIS_URL", ""),
			KeyPrefix:   getEnv("REDIS_KEY_PREFIX", "reservaloya"),
			MaxDuration: maxDuration,
		})
		if err != nil {
			log.Fatalw("occupancy_coordinator_config_error", "error", err)
		}
		healthCtx, cancelHealth := context.WithTimeout(context.Background(), 5*time.Second)
		if err := coordinator.Health(healthCtx); err != nil {
			cancelHealth()
			log.Fatalw("occupancy_coordinator_unavailable", "error", err)
		}
		cancelHealth()
		bookingUC.SetOccupancyCoordinator(coordinator)
		// Modo por defecto de los centros sin occupancy_authority propio.
		// "off" permite activar el coordinador centro por centro.
		defaultMode, ok := app.ParseOccupancyMode(getEnv("BOOKING_OCCUPANCY_MODE", string(app.OccupancyAuthority)))
		if !ok {
			log.Fatalw("invalid_booking_occupancy_mode")
		}
		bookingUC.SetOccupancyDefaultMode(defaultMode)
		log.Infow("occupancy_default_mode", "mode", string(defaultMode))

		interval, err := time.ParseDuration(getEnv("BOOKING_RECONCILE_INTERVAL", "60s"))
		if err != nil || interval <= 0 {
			log.Fatalw("invalid_booking_reconcile_interval", "error", err)
		}
		reconciler := &app.OccupancyReconciler{
			Store: mongo.NewOccupancyReconcileStore(db),
			Coord: coordinator,
			Locks: jobLockRepo,
			Apply: true,
		}
		go reconciler.Loop(context.Background(), interval)
		log.Infow("occupancy_coordinator_enabled")
	}

	log.Infow("use_cases_initialized")

	auth0Mgmt := auth0mgmt.NewManagementClient()
	if os.Getenv("AUTH0_ADMIN_ROLE_ID") == "" {
		log.Warnw("auth0_admin_role_id_not_configured", "effect", "onboard no asignará rol de administrador")
	}
	sportCenterHandler := infra.NewSportCenterHandler(sportCenterUC, auth0Mgmt, bookingMailer, facilityUC)
	courtHandler := infra.NewCourtHandler(courtUC)
	// Sedes y piscinas se reservan por día completo y viven en `bookings`, igual
	// que las canchas. `FACILITY_BOOKINGS_STORE=legacy` vuelve a la colección
	// anterior; mientras tanto el store lee también las reservas legacy.
	var facilityBookingStore app.FacilityBookingRepository = app.NewBookingBackedFacilityStore(bookingUC, legacyFacilityBookingRepo)
	if getEnv("FACILITY_BOOKINGS_STORE", "bookings") == "legacy" {
		facilityBookingStore = legacyFacilityBookingRepo
	}
	scheduleUC.SetBookedDatesSource(facilityBookingStore)
	facilityBookingUC := app.NewFacilityBookingUseCase(facilityBookingStore, spaceRepo, sportCenterRepo, scheduleUC, mercadopago.NewClient(getEnv("MP_ACCESS_TOKEN", "")))
	if facilityMailer != nil {
		facilityBookingUC.SetMailer(facilityMailer)
	}

	bookingHandler := infra.NewBookingHandler(bookingUC)
	bookingHandler.SetFacilityBookings(facilityBookingUC)
	facilityBookingHandler := infra.NewFacilityBookingHandler(facilityBookingUC)
	contactHandler := infra.NewContactHandler(bookingMailer)
	membershipHandler := infra.NewMembershipHandler(membershipUC, memberVerificationUC, sportCenterRepo)
	discountHandler := infra.NewDiscountHandler(discountUC, membershipUC)
	pricingHandler := infra.NewPricingHandler(pricingService)

	mpConnectHandler := infra.NewMPConnectHandler(mpConnectUC)

	tenantUC := app.NewTenantUseCase(sportCenterRepo)
	tenantHandler := infra.NewTenantHandler(tenantUC)

	facilityHandler := infra.NewFacilityHandler(facilityUC)
	spaceHandler := infra.NewSpaceHandler(spaceUC)
	scheduleHandler := infra.NewScheduleHandler(scheduleUC)
	userProfileHandler := infra.NewUserProfileHandler(app.NewUserProfileUseCase(userProfileRepo))
	bookingClaimHandler := infra.NewBookingClaimHandler(bookingRepo)

	log.Infow("handlers_initialized")

	ginMode := getEnv("GIN_MODE", "release")
	gin.SetMode(ginMode)
	r := gin.New()

	r.Use(middleware.Recovery())
	r.Use(middleware.Tracing())
	r.Use(middleware.NewRelicMiddleware())

	r.Use(cors.New(cors.Config{
		// White-label multi-tenant: cada cliente puede traer su propio dominio
		// (ej: www.unioncatolica.cl), por lo que no podemos hardcodear origins.
		// La autenticación es vía header Authorization (JWT) y guest_device_id.
		AllowOriginFunc: func(origin string) bool {
			return true
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Trace-ID"},
		ExposeHeaders:    []string{"Content-Length", "X-Trace-ID"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	log.Infow("cors_configured")

	r.Use(middleware.DeviceID())

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
	r.GET("/api/tenants/host/:host", tenantHandler.GetByHost)
	r.GET("/api/tenants/:subdomain", tenantHandler.GetBySubdomain)
	r.GET("/api/sport-centers", sportCenterHandler.List)
	r.GET("/api/cities", sportCenterHandler.ListCities)
	r.GET("/api/sport-centers/slug/:slug", sportCenterHandler.GetBySlug)
	r.GET("/api/sport-centers/slug/:slug/reviews", infra.NewGoogleReviewsHandler(sportCenterUC).GetBySlug)
	r.POST("/api/sport-centers", sportCenterHandler.Create)
	r.POST("/api/sport-centers/onboard", sportCenterHandler.OnboardCreate)
	r.GET("/api/sport-centers/onboard/verification-status", sportCenterHandler.OnboardVerificationStatus)
	r.PUT("/api/sport-centers/:id", sportCenterHandler.Update)
	r.GET("/api/sport-centers/:id/schedules", sportCenterHandler.GetSchedules)
	r.GET("/api/courts", courtHandler.List)
	r.POST("/api/courts", courtHandler.CreateCourt)
	r.GET("/api/courts/:id/schedule", courtHandler.GetSchedule)
	// Facilities (quinchos, sedes, piscinas)
	r.GET("/api/sport-centers/:id/facilities", facilityHandler.GetByCenterID)
	r.GET("/api/sport-centers/slug/:slug/facilities", facilityHandler.GetBySlug)
	r.POST("/api/facilities/inquiries", facilityHandler.CreateInquiry)
	// Reservas por día de sedes y piscinas
	r.POST("/api/facility-bookings", facilityBookingHandler.Create)
	r.GET("/api/spaces/:id/payment-options", facilityBookingHandler.PaymentOptions)
	// MercadoPago payment routes
	r.POST("/api/bookings/mercadopago", bookingHandler.CreateMercadoPagoPayment)
	r.POST("/api/bookings/mercadopago/webhook", bookingHandler.MercadoPagoWebhook)
	r.GET("/api/bookings/mercadopago/return", bookingHandler.MercadoPagoReturn)
	r.POST("/api/bookings/mercadopago/mock-confirm", bookingHandler.MockMercadoPagoConfirm)
	r.POST("/api/internal/sync-payment", bookingHandler.SyncConfirmedPayment)
	r.POST("/api/internal/jobs/recurring-generate", bookingHandler.GenerateRecurringBookingsInternal)
	r.GET("/api/bookings/code/:code", bookingHandler.GetByBookingCode)
	r.GET("/api/bookings/code/:code/payment-status", bookingHandler.GetPaymentStatus)
	r.POST("/api/bookings/code/:code/cancel", bookingHandler.CancelByBookingCode)
	r.POST("/api/bookings", bookingHandler.CreateBooking)
	r.POST("/api/contact", contactHandler.Submit)

	// Verificación de socios y cotización con descuentos (público, sin login)
	r.POST("/api/members/verify/request", membershipHandler.RequestVerification)
	r.POST("/api/members/verify/confirm", membershipHandler.ConfirmVerification)
	r.POST("/api/pricing/quote", pricingHandler.Quote)
	// Disponibilidad pública de un espacio (usado por facilities: quincho/sede/piscina)
	r.GET("/api/spaces/:id/availability", scheduleHandler.PublicAvailability)
	// MercadoPago Connect (marketplace) - callback público (redirección de MP)
	r.GET("/api/admin/mercadopago/callback", mpConnectHandler.Callback)

	superAdminRepo := mongo.NewSuperAdminRepository(db)
	if err := superAdminRepo.EnsureIndexes(context.Background()); err != nil {
		log.Warnw("superadmin_indexes_failed", "error", err)
	}
	superAdminHandler := infra.NewSuperAdminHandler(superAdminRepo)

	// Rutas Protegidas
	api := r.Group("/api")
	api.Use(authMiddleware)
	{
		// Registro de dispositivos para notificaciones
		api.POST("/users/devices", bookingHandler.RegisterDevice)
		// Teléfono guardado del usuario autenticado (precarga en reservas)
		api.GET("/users/me/phone", userProfileHandler.GetPhone)
		api.PUT("/users/me/phone", userProfileHandler.SavePhone)

		// Endpoint seguro para obtener schedules con detalles de reservas
		api.GET("/sport-centers/:id/schedules/bookings", sportCenterHandler.GetSchedulesWithBookings)
		// Endpoint para administradores: obtener agenda automáticamente sin pasar id
		api.GET("/admin/my-sport-center", sportCenterHandler.GetMySportCenter)
		api.PUT("/admin/my-sport-center/onboarding", sportCenterHandler.UpdateMySportCenterOnboarding)
		api.GET("/admin/sport-centers/schedules/bookings", sportCenterHandler.GetAdminSchedulesWithBookings)
		api.GET("/bookings/:id", bookingHandler.GetBookingDetail)
		api.POST("/bookings/claim", bookingClaimHandler.Claim)
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
		api.GET("/admin/facilities", facilityHandler.GetAdmin)
		api.POST("/admin/facilities", facilityHandler.Create)
		api.PUT("/admin/facilities/:id", facilityHandler.Update)
		api.DELETE("/admin/facilities/:id", facilityHandler.Delete)
		api.GET("/admin/facility-bookings", facilityBookingHandler.ListAdmin)
		api.POST("/admin/facility-bookings/:id/cancel", facilityBookingHandler.CancelAdmin)
		api.GET("/admin/spaces", spaceHandler.GetAdminSpaces)
		api.POST("/admin/spaces", spaceHandler.Create)
		api.PUT("/admin/spaces/:id", spaceHandler.Update)
		api.DELETE("/admin/spaces/:id", spaceHandler.Delete)
		// Configuración de horarios por espacio (reglas, excepciones y bloqueos)
		api.GET("/admin/spaces/:id/schedule", scheduleHandler.GetSchedule)
		api.PUT("/admin/spaces/:id/schedule", scheduleHandler.SaveSchedule)
		api.GET("/admin/spaces/:id/schedule/exceptions", scheduleHandler.ListExceptions)
		api.POST("/admin/spaces/:id/schedule/exceptions", scheduleHandler.CreateException)
		api.PUT("/admin/spaces/:id/schedule/exceptions/:exceptionId", scheduleHandler.UpdateException)
		api.DELETE("/admin/spaces/:id/schedule/exceptions/:exceptionId", scheduleHandler.DeleteException)
		api.GET("/admin/spaces/:id/blocks", scheduleHandler.ListBlocks)
		api.POST("/admin/spaces/:id/blocks", scheduleHandler.CreateBlock)
		api.DELETE("/admin/spaces/:id/blocks/:blockId", scheduleHandler.DeleteBlock)
		api.POST("/admin/bookings/internal", bookingHandler.CreateInternalBooking)
		api.POST("/admin/bookings/internal/batch", bookingHandler.CreateInternalBookingsBatch)

		// Socios y códigos de descuento
		api.GET("/admin/membership/plans", membershipHandler.ListPlans)
		api.POST("/admin/membership/plans", membershipHandler.CreatePlan)
		api.PUT("/admin/membership/plans/:id", membershipHandler.UpdatePlan)
		api.DELETE("/admin/membership/plans/:id", membershipHandler.DeletePlan)
		api.GET("/admin/members", membershipHandler.ListMembers)
		api.POST("/admin/members", membershipHandler.CreateMember)
		api.PUT("/admin/members/:id", membershipHandler.UpdateMember)
		api.DELETE("/admin/members/:id", membershipHandler.DeleteMember)
		api.GET("/admin/discount-codes", discountHandler.ListCodes)
		api.POST("/admin/discount-codes", discountHandler.CreateCode)
		api.POST("/admin/discount-codes/batch", discountHandler.CreateBatch)
		api.GET("/admin/discount-codes/export", discountHandler.ExportCodes)
		api.POST("/admin/discount-codes/:id/void", discountHandler.VoidCode)
		api.POST("/admin/discount-codes/batches/:batchId/void", discountHandler.VoidBatch)
		api.GET("/admin/discount-usages", discountHandler.ListUsages)
		api.POST("/admin/bookings/:id/verify-payment", bookingHandler.AdminVerifyPayment)
		api.POST("/admin/bookings/:id/pay-balance", bookingHandler.MarkPartialPaymentAsPaid)
		api.PATCH("/admin/bookings/:id/undo-pay-balance", bookingHandler.UndoBalancePayment)
		api.PATCH("/admin/bookings/:id/reschedule", bookingHandler.RescheduleBooking)
		api.DELETE("/admin/bookings/:id", bookingHandler.DeleteBooking)

		// Recurring Reservation routes
		api.POST("/admin/recurring", bookingHandler.CreateRecurringReservation)
		api.POST("/admin/recurring/generate", bookingHandler.GenerateRecurringBookings)
		api.GET("/admin/recurring", bookingHandler.GetRecurringReservationsByCenter)
		api.GET("/admin/recurring/:id", bookingHandler.GetRecurringReservation)
		api.GET("/admin/recurring/court/:courtId", bookingHandler.GetRecurringReservationsByCourt)
		api.DELETE("/admin/recurring/:id", bookingHandler.CancelRecurringReservation)
		api.POST("/admin/recurring/:id/cancel-date", bookingHandler.CancelRecurringDate)

		// Users management routes
		api.GET("/admin/users", sportCenterHandler.GetCenterUsers)
		api.POST("/admin/users", sportCenterHandler.InviteCenterUser)
		api.DELETE("/admin/users/:userId", sportCenterHandler.RemoveCenterUser)

		// Panel superadmin (rol "superadmin" en Auth0)
		sa := api.Group("/superadmin", middleware.RequireSuperAdmin(auth0Mgmt))
		{
			sa.GET("/me", func(c *gin.Context) { c.JSON(200, gin.H{"superadmin": true}) })
			sa.GET("/sport-centers", superAdminHandler.ListCenters)
			sa.GET("/sport-centers/stats", superAdminHandler.CenterStats)
			sa.POST("/sport-centers", superAdminHandler.Audit("center.create", "sport_center", "id"), sportCenterHandler.OnboardCreate)
			sa.GET("/sport-centers/:id", superAdminHandler.GetCenter)
			sa.PATCH("/sport-centers/:id/settings", superAdminHandler.Audit("center.settings", "sport_center", "id"), sportCenterHandler.UpdateSportCenterSettings)
			sa.PUT("/sport-centers/:id/status", superAdminHandler.SetStatus)
			sa.PUT("/sport-centers/:id/featured", superAdminHandler.SetFeatured)
			sa.GET("/bookings", superAdminHandler.ListBookings)
			sa.GET("/bookings/export", superAdminHandler.ExportBookings)
			sa.GET("/audit", superAdminHandler.ListAudit)
		}

		// MercadoPago Connect (marketplace) routes
		api.GET("/admin/mercadopago/connect", mpConnectHandler.GetAuthorizationURL)
		api.GET("/admin/mercadopago/status", mpConnectHandler.GetStatus)
		api.POST("/admin/mercadopago/disconnect", mpConnectHandler.Disconnect)
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
