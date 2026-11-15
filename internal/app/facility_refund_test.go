package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/mercadopago"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type refundRepo struct {
	FacilityBookingRepository
	group     []domain.FacilityBooking
	cancelled []primitive.ObjectID
}

func (r *refundRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*domain.FacilityBooking, error) {
	for i := range r.group {
		if r.group[i].ID == id {
			return &r.group[i], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *refundRepo) FindByCode(ctx context.Context, code string) ([]domain.FacilityBooking, error) {
	return r.group, nil
}

func (r *refundRepo) CancelByID(ctx context.Context, id primitive.ObjectID, by string) error {
	r.cancelled = append(r.cancelled, id)
	return nil
}

type refundCapture struct {
	path string
	body string
}

func (c *refundCapture) Do(req *http.Request) (*http.Response, error) {
	c.path = req.URL.Path
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		c.body = string(b)
	}
	return &http.Response{
		StatusCode: 201,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":9,"status":"approved","amount":1}`)),
	}, nil
}

func runFacilityCancel(t *testing.T, days int) (*refundCapture, *refundRepo) {
	t.Helper()
	cap := &refundCapture{}
	orig := newMPClient
	newMPClient = func(token string) *mercadopago.Client { return mercadopago.NewClientWithRequester(token, cap) }
	defer func() { newMPClient = orig }()

	center := oauthCenter(5)
	center.ID = primitive.NewObjectID()
	center.Users = []string{"admin-1"}

	var group []domain.FacilityBooking
	for i := 0; i < days; i++ {
		group = append(group, domain.FacilityBooking{
			ID: primitive.NewObjectID(), BookingCode: "FAC-1", SportCenterID: center.ID,
			Status: domain.BookingStatusConfirmed, Active: true,
			PaymentMethod: FacilityPaymentOnline, MPPaymentID: "123", PaidAmount: 10000,
		})
	}
	repo := &refundRepo{group: group}
	uc := NewFacilityBookingUseCase(repo, nil, &fakeFacilityCenterRepo{center: center}, nil, nil)
	if err := uc.CancelByAdmin(context.Background(), group[0].ID, "admin-1"); err != nil {
		t.Fatalf("CancelByAdmin: %v", err)
	}
	if len(repo.cancelled) != 1 {
		t.Fatalf("debe cancelar la reserva tras reembolsar")
	}
	return cap, repo
}

func TestFacilityCancelRefundsSurchargePartial(t *testing.T) {
	cap, _ := runFacilityCancel(t, 2)
	if !strings.HasSuffix(cap.path, "/payments/123/refunds") || !strings.Contains(cap.body, "10500") {
		t.Fatalf("reembolso parcial esperado de 10500 (precio + recargo): %s %s", cap.path, cap.body)
	}
}

func TestFacilityCancelRefundsFullWhenLastDay(t *testing.T) {
	cap, _ := runFacilityCancel(t, 1)
	if !strings.HasSuffix(cap.path, "/payments/123/refunds") || strings.Contains(cap.body, "amount") {
		t.Fatalf("reembolso total esperado: %s %s", cap.path, cap.body)
	}
}
