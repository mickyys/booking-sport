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
	logFile, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		multiWriter := io.MultiWriter(os.Stdout, logFile)
		log.SetOutput(multiWriter)
		gin.DefaultWriter = multiWriter
		gin.DefaultErrorWriter = multiWriter

		log.Println("--- Application Start ---")
		log.Printf("Logging to app.log and stdout\n")
	} else {
		fmt.Printf("Error opening log file: %v\n", err)
	}

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

	if err := mongo.EnsureIndexes(ctx, db); err != nil {
		log.Printf("Warning: Error creando índices de MongoDB: %v", err)
	}

	sportCenterRepo := mongo.NewSportCenterRepository(db)
	if err := sportCenterRepo.SyncCourtsCount(ctx); err != nil {
		log.Printf("Warning: Error sincronizando contador de canchas: %v", err)
	}
	courtRepo := mongo.NewCourtRepository(db)
	userRepo := mongo.NewUserRepository(db)
	bookingRepo := mongo.NewBookingRepository(db)

	sportCenterUC := app.NewSportCenterUseCase(sportCenterRepo, courtRepo, userRepo, bookingRepo)
	courtUC := app.NewCourtUseCase(courtRepo, sportCenterRepo, bookingRepo)
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

	bookingUC := app.NewBookingUseCase(bookingRepo, courtRepo, sportCenterRepo, userRepo, bookingMailer)

	sportCenterHandler := infra.NewSportCenterHandler(sportCenterUC)
	courtHandler := infra.NewCourtHandler(courtUC)
	bookingHandler := infra.NewBookingHandler(bookingUC)
	contactHandler := infra.NewContactHandler(bookingMailer)

	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
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

	authMiddleware := auth.EnsureValidToken(
		os.Getenv("AUTH0_DOMAIN"),
		os.Getenv("AUTH0_AUDIENCE"),
	)

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
	r.POST("/api/bookings/mercadopago", bookingHandler.CreateMercadoPagoPayment)
	r.POST("/api/bookings/mercadopago/webhook", bookingHandler.MercadoPagoWebhook)
	r.GET("/api/bookings/mercadopago/return", bookingHandler.MercadoPagoReturn)
	r.GET("/api/bookings/code/:code", bookingHandler.GetByBookingCode)
	r.POST("/api/bookings/code/:code/cancel", bookingHandler.CancelByBookingCode)
	r.POST("/api/bookings", bookingHandler.CreateBooking)
	r.POST("/api/contact", contactHandler.Submit)

	api := r.Group("/api")
	api.Use(authMiddleware)
	{
		api.GET("/sport-centers/:id/schedules/bookings", sportCenterHandler.GetSchedulesWithBookings)
		api.GET("/bookings/:id", bookingHandler.GetBookingDetail)
		api.GET("/bookings/my-bookings", bookingHandler.GetUserBookings)
		api.GET("/bookings/my-cancelled", bookingHandler.GetUserCancelledBookings)
		api.GET("/bookings/confirmed/count", bookingHandler.GetConfirmedCount)
		api.POST("/bookings/:id/cancel", bookingHandler.CancelBooking)
		api.POST("/bookings/:id/pay-in-person", bookingHandler.MarkAsPaidInPerson)
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
		api.PATCH("/admin/sport-centers/:id/settings", sportCenterHandler.UpdateSettings)
		api.GET("/admin/sport-centers/:id", sportCenterHandler.GetByID)
		api.POST("/admin/bookings/internal", bookingHandler.CreateInternalBooking)
		api.DELETE("/admin/bookings/:id", bookingHandler.DeleteBooking)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Servidor escuchando en puerto %s...\n", port)
	log.Fatal(r.Run(":" + port))
}
