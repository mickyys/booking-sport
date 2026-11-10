package infra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type fakeGuestClaimer struct {
	calls   int
	email   string
	userID  string
	claimed int64
	err     error
}

func (f *fakeGuestClaimer) ClaimByEmail(_ context.Context, email, userID string) (int64, error) {
	f.calls++
	f.email = email
	f.userID = userID
	return f.claimed, f.err
}

type fakeEmailLookup struct {
	calls int
	user  string
	email string
	err   error
}

func (f *fakeEmailLookup) EmailByUserID(_ context.Context, userID string) (string, error) {
	f.calls++
	f.user = userID
	return f.email, f.err
}

func newClaimRouter(t *testing.T, h *BookingClaimHandler, userID, userEmail string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if userID != "" {
			c.Set("user_id", userID)
		}
		if userEmail != "" {
			c.Set("user_email", userEmail)
		}
		c.Next()
	})
	r.POST("/bookings/claim", h.Claim)
	return r
}

func doClaim(t *testing.T, r *gin.Engine) (int, map[string]interface{}) {
	t.Helper()
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/bookings/claim", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	r.ServeHTTP(w, req)

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
	return w.Code, body
}

func TestBookingClaim_UsesTokenEmail(t *testing.T) {
	claimer := &fakeGuestClaimer{claimed: 1}
	lookup := &fakeEmailLookup{email: "otro@yopmail.com"}
	h := NewBookingClaimHandler(claimer, lookup)
	r := newClaimRouter(t, h, "auth0|6af2991fd0bd0cd90b63b53f", "catolica1@yopmail.com")

	status, body := doClaim(t, r)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got := body["claimed"]; got != float64(1) {
		t.Fatalf("claimed = %v, want 1", got)
	}
	if claimer.email != "catolica1@yopmail.com" {
		t.Fatalf("ClaimByEmail email = %q, want el del token", claimer.email)
	}
	if claimer.userID != "auth0|6af2991fd0bd0cd90b63b53f" {
		t.Fatalf("ClaimByEmail userID = %q", claimer.userID)
	}
	if lookup.calls != 0 {
		t.Fatalf("lookup.calls = %d, want 0 cuando el token trae email", lookup.calls)
	}
}

func TestBookingClaim_ResolvesEmailFromAuth0WhenTokenHasNone(t *testing.T) {
	claimer := &fakeGuestClaimer{claimed: 2}
	lookup := &fakeEmailLookup{email: "catolica1@yopmail.com"}
	h := NewBookingClaimHandler(claimer, lookup)
	r := newClaimRouter(t, h, "auth0|6af2991fd0bd0cd90b63b53f", "")

	status, body := doClaim(t, r)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got := body["claimed"]; got != float64(2) {
		t.Fatalf("claimed = %v, want 2", got)
	}
	if lookup.user != "auth0|6af2991fd0bd0cd90b63b53f" {
		t.Fatalf("lookup.user = %q", lookup.user)
	}
	if claimer.email != "catolica1@yopmail.com" {
		t.Fatalf("ClaimByEmail email = %q, want el resuelto en Auth0", claimer.email)
	}
}

func TestBookingClaim_FailsLoudlyWhenEmailIsUnavailable(t *testing.T) {
	claimer := &fakeGuestClaimer{claimed: 1}
	lookup := &fakeEmailLookup{err: context.DeadlineExceeded}
	h := NewBookingClaimHandler(claimer, lookup)
	r := newClaimRouter(t, h, "auth0|6af2991fd0bd0cd90b63b53f", "")

	status, body := doClaim(t, r)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got := body["claimed"]; got != float64(0) {
		t.Fatalf("claimed = %v, want 0", got)
	}
	if got := body["reason"]; got != "email_unavailable" {
		t.Fatalf("reason = %v, want email_unavailable", got)
	}
	if claimer.calls != 0 {
		t.Fatalf("ClaimByEmail no debe llamarse sin correo (calls = %d)", claimer.calls)
	}
}

func TestBookingClaim_EmptyEmailFromAuth0DoesNotClaim(t *testing.T) {
	claimer := &fakeGuestClaimer{claimed: 1}
	lookup := &fakeEmailLookup{email: ""}
	h := NewBookingClaimHandler(claimer, lookup)
	r := newClaimRouter(t, h, "auth0|6af2991fd0bd0cd90b63b53f", "")

	status, body := doClaim(t, r)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got := body["reason"]; got != "email_unavailable" {
		t.Fatalf("reason = %v, want email_unavailable", got)
	}
	if claimer.calls != 0 {
		t.Fatalf("ClaimByEmail no debe llamarse sin correo (calls = %d)", claimer.calls)
	}
}

func TestBookingClaim_RejectsRequestWithoutUser(t *testing.T) {
	h := NewBookingClaimHandler(&fakeGuestClaimer{}, &fakeEmailLookup{})
	r := newClaimRouter(t, h, "", "")

	status, _ := doClaim(t, r)

	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
}
