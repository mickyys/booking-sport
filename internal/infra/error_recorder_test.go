package infra

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/domain"
)

type fakeErrorStore struct {
	items []*domain.BookingErrorLog
	dups  int
}

func (f *fakeErrorStore) Insert(_ context.Context, l *domain.BookingErrorLog) (bool, error) {
	if f.dups > 0 {
		f.dups--
		return true, nil
	}
	cp := *l
	f.items = append(f.items, &cp)
	return false, nil
}

func TestSanitizeErrorText(t *testing.T) {
	in := "fail APP_USR-1234567890-abcdef Bearer abc.def access_token=secret123 eyJhbGci.eyJzdWIi.sig_1"
	out := sanitizeErrorText(in)
	for _, leak := range []string{"APP_USR-1234567890", "abc.def", "secret123", "eyJhbGci"} {
		if strings.Contains(out, leak) {
			t.Fatalf("filtró %q: %s", leak, out)
		}
	}
	if got := sanitizeErrorText(strings.Repeat("x", 10000)); len(got) > maxErrorMessageLen+4 {
		t.Fatalf("no truncó: %d", len(got))
	}
}

func TestErrorRecorderRecord(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &fakeErrorStore{dups: 1}
	rec := NewErrorRecorder(store)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/bookings/mercadopago", nil)
	c.Set("user_id", "auth0|1")
	c.Set("user_email", "a@b.cl")

	id := rec.Record(c, domain.ErrOpPaymentCreate, errors.New("boom"), 500, ErrorContext{BookingCode: "abc"})
	if !strings.HasPrefix(id, "ERR-") || len(id) != 12 {
		t.Fatalf("tracking id inválido: %q", id)
	}
	if len(store.items) != 1 || store.items[0].UserEmail != "a@b.cl" || store.items[0].UserID != "auth0|1" {
		t.Fatalf("registro inesperado: %+v", store.items)
	}
}

func TestErrorRecorderNilSafe(t *testing.T) {
	var rec *ErrorRecorder
	if id := rec.Record(nil, "x", errors.New("e"), 500, ErrorContext{}); id != "" {
		t.Fatal("esperaba vacío")
	}
	rec.RecordBackground("", "x", errors.New("e"), ErrorContext{})
}
