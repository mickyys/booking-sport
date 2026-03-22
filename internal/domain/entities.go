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

type SportCenter struct {
	ID                primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Name              string             `bson:"name" json:"name"`
	Address           string             `bson:"address" json:"address"`
	Coordinates       Coordinates        `bson:"coordinates" json:"coordinates"`
	Services          []string           `bson:"services" json:"services"`
	Contact           Contact            `bson:"contact" json:"contact"`
	Fintoc            *FintocConfig      `bson:"fintoc,omitempty" json:"-"` // Ocultar datos privados de Fintoc
	CancellationHours int                `bson:"cancellation_hours" json:"cancellation_hours"`
	RetentionPercent  int                `bson:"retention_percent" json:"retention_percent"`
	Users             []string           `bson:"users" json:"users"` // Usuarios asociados al centro
	CreatedAt         time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt         time.Time          `bson:"updated_at" json:"updated_at"`
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
	Hour    int     `bson:"hour" json:"hour"`       // 0 - 23
	Minutes int     `bson:"minutes" json:"minutes"` // 0 - 59
	Price   float64 `bson:"price" json:"price"`     // Valor por hora
	Status  string  `bson:"status" json:"status"`   // "available", "booked", "closed"
}

type User struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Username  string             `bson:"username" json:"username"`
	Password  string             `bson:"password" json:"-"` // Ocultar password en JSON
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
	ID                primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Date              time.Time          `bson:"date" json:"date"`
	Hour              int                `bson:"hour" json:"hour"`
	SportCenterName   string             `bson:"sport_center_name" json:"sport_center_name"`
	CourtName         string             `bson:"court_name" json:"court_name"`
	Status            BookingStatus      `bson:"status" json:"status"`
	Price             float64            `bson:"price" json:"price"`
	PaymentMethod     string             `bson:"payment_method" json:"payment_method"`
	CancellationHours int                `bson:"cancellation_hours" json:"cancellation_hours"`
	RetentionPercent  int                `bson:"retention_percent" json:"retention_percent"`
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
	Status                BookingStatus      `bson:"status" json:"status"`
	BookingCode           string             `bson:"booking_code,omitempty" json:"booking_code,omitempty"`
	PaymentMethod         string             `bson:"payment_method,omitempty" json:"payment_method,omitempty"`
	PaymentID             string             `bson:"payment_id,omitempty" json:"payment_id,omitempty"`
	FintocPaymentID       string             `bson:"fintoc_payment_id,omitempty" json:"fintoc_payment_id,omitempty"`
	FintocPaymentIntentID string             `bson:"fintoc_payment_intent_id,omitempty" json:"fintoc_payment_intent_id,omitempty"`
	Refunds               []Refund           `bson:"refunds,omitempty" json:"refunds,omitempty"`
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
