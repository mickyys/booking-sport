package mercadopago

import (
	"context"
	"fmt"
	"time"

	"github.com/mercadopago/sdk-go/pkg/preapproval"
)

// SubscriptionInput describe la suscripción mensual de un centro a ReservaloYA.
type SubscriptionInput struct {
	Reason      string
	ExternalRef string
	PayerEmail  string
	BackURL     string
	Amount      float64
	Currency    string
}

// SubscriptionResult es el estado de una suscripción (preapproval) en MP.
type SubscriptionResult struct {
	ID          string
	Status      string
	ExternalRef string
	InitPoint   string
	NextPayment *time.Time
}

func toSubscriptionResult(r *preapproval.Response) *SubscriptionResult {
	res := &SubscriptionResult{ID: r.ID, Status: r.Status, ExternalRef: r.ExternalReference, InitPoint: r.InitPoint}
	if !r.NextPaymentDate.IsZero() {
		next := r.NextPaymentDate
		res.NextPayment = &next
	}
	return res
}

// CreateSubscription crea un preapproval mensual en estado pending. El usuario
// lo autoriza en InitPoint.
func (c *Client) CreateSubscription(ctx context.Context, in SubscriptionInput) (*SubscriptionResult, error) {
	cfg, err := c.getConfig()
	if err != nil {
		return nil, err
	}
	currency := in.Currency
	if currency == "" {
		currency = "CLP"
	}
	res, err := preapproval.NewClient(cfg).Create(ctx, preapproval.Request{
		Reason:            in.Reason,
		ExternalReference: in.ExternalRef,
		PayerEmail:        in.PayerEmail,
		BackURL:           in.BackURL,
		Status:            "pending",
		AutoRecurring: &preapproval.AutoRecurringRequest{
			Frequency:         1,
			FrequencyType:     "months",
			TransactionAmount: in.Amount,
			CurrencyID:        currency,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error creating mercadopago subscription: %w", err)
	}
	return toSubscriptionResult(res), nil
}

// GetSubscription consulta el estado vigente de un preapproval.
func (c *Client) GetSubscription(ctx context.Context, id string) (*SubscriptionResult, error) {
	cfg, err := c.getConfig()
	if err != nil {
		return nil, err
	}
	res, err := preapproval.NewClient(cfg).Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("error getting mercadopago subscription: %w", err)
	}
	return toSubscriptionResult(res), nil
}
