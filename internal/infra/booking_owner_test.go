package infra

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/domain"
)

func newBookingContext() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/bookings/mercadopago", nil)
	return c
}

func TestSetBookingOwnerFromToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("el sub del token manda sobre el user_id del cliente", func(t *testing.T) {
		h := &BookingHandler{baseHandler: NewBaseHandler()}
		c := newBookingContext()
		c.Set("user_id", "auth0|6af2991fd0bd0cd90b63b53f")
		booking := &domain.Booking{UserID: "auth0|otra-cuenta"}

		h.setBookingOwnerFromToken(c, booking)

		if booking.UserID != "auth0|6af2991fd0bd0cd90b63b53f" {
			t.Fatalf("UserID = %q, want el sub del token", booking.UserID)
		}
	})

	t.Run("sin token se conserva el user_id del body (checkout web)", func(t *testing.T) {
		h := &BookingHandler{baseHandler: NewBaseHandler()}
		c := newBookingContext()
		booking := &domain.Booking{UserID: "auth0|cliente-web"}

		h.setBookingOwnerFromToken(c, booking)

		if booking.UserID != "auth0|cliente-web" {
			t.Fatalf("UserID = %q, want el del body", booking.UserID)
		}
	})

	t.Run("token con user_id vacío no pisa el body", func(t *testing.T) {
		h := &BookingHandler{baseHandler: NewBaseHandler()}
		c := newBookingContext()
		c.Set("user_id", "   ")
		booking := &domain.Booking{UserID: "auth0|cliente-web"}

		h.setBookingOwnerFromToken(c, booking)

		if booking.UserID != "auth0|cliente-web" {
			t.Fatalf("UserID = %q, want el del body", booking.UserID)
		}
	})
}
