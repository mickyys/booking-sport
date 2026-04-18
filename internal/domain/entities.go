package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type UserRole string

const (
	RoleAdmin UserRole = "admin"
	RoleUser  UserRole = "user"
)

type Coordinates struct {
	Lat float64 `bson:"lat" json:"lat"`
	Lng float64 `bson:"lng" json:"lng"`
}

type Contact struct {
	Phone string `bson:"phone" json:"phone"`
	Email string `bson:"email" json:"email"`
}

type FintocPaymentConfig struct {
	SecretKey string `bson:"secret_key" json:"secret_key"`
}

type FintocWebhookConfig struct {
	ID        string `bson:"id" json:"id"`
	SecretKey string `bson:"secret_key" json:"secret_key"`
}

type FintocConfig struct {
	Payment FintocPaymentConfig `bson:"payment" json:"payment"`
	Webhook FintocWebhookConfig `bson:"webhook" json:"webhook"`
}

type MercadoPagoConfig struct {
	AccessToken string `bson:"access_token" json:"access_token"`
}

type SportCenter struct {
	ID                     primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Slug                   string             `bson:"slug" json:"slug"`
	Name                   string             `bson:"name" json:"name"`
	City                   string             `bson:"city" json:"city"`
	Address                string             `bson:"address" json:"address"`
	Coordinates            Coordinates        `bson:"coordinates" json:"coordinates"`
	Services               []string           `bson:"services" json:"services"`
	Contact                Contact            `bson:"contact" json:"contact"`
	CourtsCount            int                `bson:"courts_count" json:"courts"`
	Fintoc                 *FintocConfig      `bson:"fintoc,omitempty" json:"-"`
	MercadoPago            *MercadoPagoConfig `bson:"mercadopago,omitempty" json:"-"`
	CancellationHours      int                `bson:"cancellation_hours" json:"cancellation_hours"`
	RetentionPercent       int                `bson:"retention_percent" json:"retention_percent"`
	PartialPaymentEnabled  bool               `bson:"partial_payment_enabled" json:"partial_payment_enabled"`
	PartialPaymentPercent  int                `bson:"partial_payment_percent" json:"partial_payment_percent"`
	Users                  []string           `bson:"users" json:"users"`
	CreatedAt              time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt              time.Time          `bson:"updated_at" json:"updated_at"`
}

type Court struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	SportCenterID primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	Name          string             `bson:"name" json:"name"`
	Description   string             `bson:"description" json:"description"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at" json:"updated_at"`
	Schedule      []CourtSchedule    `bson:"schedule" json:"schedule"`
}

type CourtSchedule struct {
	Hour                  int     `bson:"hour" json:"hour"`
	Minutes               int     `bson:"minutes" json:"minutes"`
	Price                 float64 `bson:"price" json:"price"`
	Status                string  `bson:"status" json:"status"`
	PaymentRequired       bool    `bson:"payment_required" json:"payment_required"`
	PaymentOptional       bool    `bson:"payment_optional" json:"payment_optional"`
	PartialPaymentEnabled *bool   `bson:"partial_payment_enabled" json:"partial_payment_enabled"`
}

type User struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Username  string             `bson:"username" json:"username"`
	Password  string             `bson:"password" json:"-"`
	Role      UserRole           `bson:"role" json:"role"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
}

type PagedResponse struct {
	Data       interface{} `json:"data"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	Limit      int         `json:"limit"`
	TotalPages int         `json:"total_pages"`
}

type BookingSummary struct {
	ID                 primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	SportCenterName    string             `bson:"sport_center_name" json:"sport_center_name"`
	CustomerName       string             `bson:"customer_name" json:"customer_name"`
	CustomerPhone      string             `bson:"customer_phone" json:"customer_phone"`
	CustomerEmail      string             `bson:"customer_email" json:"customer_email"`
	BookingCode        string             `bson:"booking_code" json:"booking_code"`
	Date               time.Time          `bson:"date" json:"date"`
	Hour               int                `bson:"hour" json:"hour"`
	CourtName          string             `bson:"court_name" json:"court_name"`
	Status             BookingStatus      `bson:"status" json:"status"`
	Price              float64            `bson:"price" json:"price"`
	FinalPrice         float64            `bson:"final_price" json:"final_price"`
	PaidAmount         float64            `bson:"paid_amount" json:"paid_amount"`
	PendingAmount      float64            `bson:"pending_amount" json:"pending_amount"`
	IsPartialPayment   bool               `bson:"is_partial_payment" json:"is_partial_payment"`
	PartialPaymentPaid bool               `bson:"partial_payment_paid" json:"partial_payment_paid"`
	PaymentMethod      string             `bson:"payment_method" json:"payment_method"`
	CancellationHours  int                `bson:"cancellation_hours" json:"cancellation_hours"`
	RetentionPercent   int                `bson:"retention_percent" json:"retention_percent"`
}

type RecurringSeries struct {
	SeriesID      string    `bson:"_id" json:"series_id"`
	CustomerName  string    `bson:"customer_name" json:"customer_name"`
	CustomerPhone string    `bson:"customer_phone" json:"customer_phone"`
	CourtName     string    `bson:"court_name" json:"court_name"`
	Hour          int       `bson:"hour" json:"hour"`
	StartDate     time.Time `bson:"start_date" json:"start_date"`
	EndDate       time.Time `bson:"end_date" json:"end_date"`
	BookingsCount int       `bson:"bookings_count" json:"bookings_count"`
}

type AdminDashboardData struct {
	TodayBookingsCount int              `json:"today_bookings_count"`
	TodayRevenue       float64          `json:"today_revenue"`
	TodayOnlineRevenue float64          `json:"today_online_revenue"`
	TodayVenueRevenue  float64          `json:"today_venue_revenue"`
	TotalRevenue       float64          `json:"total_revenue"`
	TotalOnlineRevenue float64          `json:"total_online_revenue"`
	TotalVenueRevenue  float64          `json:"total_venue_revenue"`
	CancelledCount     int              `json:"cancelled_count"`
	RecentBookings     []BookingSummary `json:"recent_bookings"`
	TotalRecentCount   int64            `json:"total_recent_count"`
	Page               int              `json:"page"`
	Limit              int              `json:"limit"`
	TotalPages         int              `json:"total_pages"`
}

type BookingStatus string

const (
	BookingStatusPending   BookingStatus = "pending"
	BookingStatusConfirmed BookingStatus = "confirmed"
	BookingStatusCancelled BookingStatus = "cancelled"
)

type Booking struct {
	ID                    primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	CourtID               primitive.ObjectID `bson:"court_id" json:"court_id"`
	SportCenterID         primitive.ObjectID `bson:"sport_center_id" json:"sport_center_id"`
	CourtName             string             `bson:"court_name,omitempty" json:"court_name,omitempty"`
	SportCenterName       string             `bson:"sport_center_name,omitempty" json:"sport_center_name,omitempty"`
	UserID                string             `bson:"user_id,omitempty" json:"user_id,omitempty"`
	GuestDetails          *GuestDetails      `bson:"guest_details,omitempty" json:"guest_details,omitempty"`
	Date                  time.Time          `bson:"date" json:"date"`
	Hour                  int                `bson:"hour" json:"hour"`
	FinalPrice            float64            `bson:"final_price" json:"final_price"`
	Price                 float64            `bson:"price" json:"price"`
	PaidAmount            float64            `bson:"paid_amount" json:"paid_amount"`
	PendingAmount         float64            `bson:"pending_amount" json:"pending_amount"`
	IsPartialPayment      bool               `bson:"is_partial_payment" json:"is_partial_payment"`
	PartialPaymentPaid    bool               `bson:"partial_payment_paid" json:"partial_payment_paid"`
	Status                BookingStatus      `bson:"status" json:"status"`
	BookingCode           string             `bson:"booking_code,omitempty" json:"booking_code,omitempty"`
	PaymentMethod         string             `bson:"payment_method,omitempty" json:"payment_method,omitempty"`
	PaymentID             string             `bson:"payment_id,omitempty" json:"payment_id,omitempty"`
	FintocPaymentID       string             `bson:"fintoc_payment_id,omitempty" json:"fintoc_payment_id,omitempty"`
	FintocPaymentIntentID string             `bson:"fintoc_payment_intent_id,omitempty" json:"fintoc_payment_intent_id,omitempty"`
	MPPreferenceID        string             `bson:"mp_preference_id,omitempty" json:"mp_preference_id,omitempty"`
	MPPaymentID           string             `bson:"mp_payment_id,omitempty" json:"mp_payment_id,omitempty"`
	Refunds               []Refund           `bson:"refunds,omitempty" json:"refunds,omitempty"`
	CancelledBy           string             `bson:"cancelled_by,omitempty" json:"cancelled_by,omitempty"`
	CancellationReason    string             `bson:"cancellation_reason,omitempty" json:"cancellation_reason,omitempty"`
	CustomerName          string             `bson:"customer_name,omitempty" json:"customer_name,omitempty"`
	CustomerPhone         string             `bson:"customer_phone,omitempty" json:"customer_phone,omitempty"`
	SeriesID              string             `bson:"series_id,omitempty" json:"series_id,omitempty"`
	RecurringID           string             `bson:"recurring_id,omitempty" json:"recurring_id,omitempty"`
	ModifiedBy            string             `bson:"modified_by,omitempty" json:"modified_by,omitempty"`
	ModifiedAt            *time.Time         `bson:"modified_at,omitempty" json:"modified_at,omitempty"`
	CreatedAt             time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt             time.Time          `bson:"updated_at" json:"updated_at"`
}

type Refund struct {
	ID        string    `bson:"id" json:"id"`
	Amount    int       `bson:"amount" json:"amount"`
	Status    string    `bson:"status" json:"status"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
}

type GuestDetails struct {
	Name  string `bson:"name" json:"name"`
	Email string `bson:"email" json:"email"`
	Phone string `bson:"phone" json:"phone"`
}
