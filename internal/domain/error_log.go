package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Operaciones que registra el log de errores de pago y reserva.
const (
	ErrOpPaymentCreate     = "payment_create"
	ErrOpPaymentWebhook    = "payment_webhook"
	ErrOpBookingCreate     = "booking_create"
	ErrOpBookingCancel     = "booking_cancel"
	ErrOpFacilityCreate    = "facility_booking_create"
	ErrOpBookingReschedule = "booking_reschedule"
)

// BookingErrorLog es un error ocurrido al pagar o ejecutar una acción sobre
// una reserva. Se muestra al usuario su TrackingID y el superadmin lo usa para
// ubicar el detalle. Se elimina solo (índice TTL sobre CreatedAt).
type BookingErrorLog struct {
	ID               primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TrackingID       string             `bson:"tracking_id" json:"tracking_id"`
	TraceID          string             `bson:"trace_id,omitempty" json:"trace_id,omitempty"`
	Operation        string             `bson:"operation" json:"operation"`
	Message          string             `bson:"message" json:"message"`
	HTTPStatus       int                `bson:"http_status" json:"http_status"`
	UserID           string             `bson:"user_id,omitempty" json:"user_id,omitempty"`
	UserEmail        string             `bson:"user_email,omitempty" json:"user_email,omitempty"`
	UserName         string             `bson:"user_name,omitempty" json:"user_name,omitempty"`
	UserPhone        string             `bson:"user_phone,omitempty" json:"user_phone,omitempty"`
	BookingID        string             `bson:"booking_id,omitempty" json:"booking_id,omitempty"`
	BookingCode      string             `bson:"booking_code,omitempty" json:"booking_code,omitempty"`
	SportCenterID    string             `bson:"sport_center_id,omitempty" json:"sport_center_id,omitempty"`
	CourtID          string             `bson:"court_id,omitempty" json:"court_id,omitempty"`
	MPPaymentID      string             `bson:"mp_payment_id,omitempty" json:"mp_payment_id,omitempty"`
	Method           string             `bson:"method,omitempty" json:"method,omitempty"`
	Path             string             `bson:"path,omitempty" json:"path,omitempty"`
	UserAgent        string             `bson:"user_agent,omitempty" json:"user_agent,omitempty"`
	IP               string             `bson:"ip,omitempty" json:"ip,omitempty"`
	Client           string             `bson:"client,omitempty" json:"client,omitempty"`
	ProviderResponse string             `bson:"provider_response,omitempty" json:"provider_response,omitempty"`
	Resolved         bool               `bson:"resolved" json:"resolved"`
	ResolvedBy       string             `bson:"resolved_by,omitempty" json:"resolved_by,omitempty"`
	ResolvedAt       *time.Time         `bson:"resolved_at,omitempty" json:"resolved_at,omitempty"`
	CreatedAt        time.Time          `bson:"created_at" json:"created_at"`
}
