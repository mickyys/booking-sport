package mercadopago

import (
	"context"
	"fmt"
	"log"

	mp "github.com/mercadopago/sdk-go/pkg/config"
	"github.com/mercadopago/sdk-go/pkg/payment"
	"github.com/mercadopago/sdk-go/pkg/preference"
	"github.com/mercadopago/sdk-go/pkg/refund"
)

type RefundResult struct {
	ID     int
	Status string
	Amount float64
}

type Client struct {
	accessToken string
}

func NewClient(accessToken string) *Client {
	return &Client{accessToken: accessToken}
}

type PreferenceResult struct {
	ID        string
	InitPoint string
}

func (c *Client) CreatePreference(ctx context.Context, title string, amount float64, email string, payerName string, payerSurname string, externalRef string, successURL string, failureURL string, pendingURL string, notificationURL string) (*PreferenceResult, error) {
	cfg, err := mp.New(c.accessToken)
	if err != nil {
		return nil, fmt.Errorf("error creating mercadopago config: %w", err)
	}

	prefClient := preference.NewClient(cfg)

	quantity := 1
	req := preference.Request{
		Items: []preference.ItemRequest{
			{
				Title:     title,
				Quantity:  quantity,
				UnitPrice: amount,
			},
		},
		Payer: &preference.PayerRequest{
			Email:   email,
			Name:    payerName,
			Surname: payerSurname,
		},
		BackURLs: &preference.BackURLsRequest{
			Success: successURL,
			Failure: failureURL,
			Pending: pendingURL,
		},
		ExternalReference: externalRef,
		AutoReturn:        "approved",
		NotificationURL:   notificationURL,
	}

	result, err := prefClient.Create(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("error creating mercadopago preference: %w", err)
	}

	log.Printf("[MERCADOPAGO] Preference created: ID=%s, InitPoint=%s\n", result.ID, result.InitPoint)

	return &PreferenceResult{
		ID:        result.ID,
		InitPoint: result.InitPoint,
	}, nil
}

func (c *Client) GetPayment(ctx context.Context, paymentID int) (*payment.Response, error) {
	cfg, err := mp.New(c.accessToken)
	if err != nil {
		return nil, fmt.Errorf("error creating mercadopago config: %w", err)
	}

	payClient := payment.NewClient(cfg)

	result, err := payClient.Get(ctx, paymentID)
	if err != nil {
		return nil, fmt.Errorf("error getting mercadopago payment: %w", err)
	}

	log.Printf("[MERCADOPAGO] Payment %d status: %s\n", paymentID, result.Status)
	return result, nil
}

// CreateRefund crea un reembolso total para un pago
func (c *Client) CreateRefund(ctx context.Context, paymentID int) (*RefundResult, error) {
	log.Println("[MERCADOPAGO] Creating refund for payment ID:", paymentID)
	log.Printf("Using access token: %s\n", c.accessToken)
	cfg, err := mp.New(c.accessToken)
	if err != nil {
		return nil, fmt.Errorf("error creating mercadopago config: %w", err)
	}

	refundClient := refund.NewClient(cfg)
	result, err := refundClient.Create(ctx, paymentID)
	if err != nil {
		return nil, fmt.Errorf("error creating mercadopago refund for payment %d: %w", paymentID, err)
	}

	log.Printf("[MERCADOPAGO] Refund created: ID=%d, PaymentID=%d, Amount=%.2f, Status=%s\n",
		result.ID, paymentID, result.Amount, result.Status)

	return &RefundResult{
		ID:     result.ID,
		Status: result.Status,
		Amount: result.Amount,
	}, nil
}

// CreatePartialRefund crea un reembolso parcial para un pago
func (c *Client) CreatePartialRefund(ctx context.Context, paymentID int, amount float64) (*RefundResult, error) {
	cfg, err := mp.New(c.accessToken)
	if err != nil {
		return nil, fmt.Errorf("error creating mercadopago config: %w", err)
	}

	refundClient := refund.NewClient(cfg)
	result, err := refundClient.CreatePartialRefund(ctx, paymentID, amount)
	if err != nil {
		return nil, fmt.Errorf("error creating partial mercadopago refund for payment %d: %w", paymentID, err)
	}

	log.Printf("[MERCADOPAGO] Partial refund created: ID=%d, PaymentID=%d, Amount=%.2f, Status=%s\n",
		result.ID, paymentID, result.Amount, result.Status)

	return &RefundResult{
		ID:     result.ID,
		Status: result.Status,
		Amount: result.Amount,
	}, nil
}

func (c *Client) SearchPaymentsByExternalReference(ctx context.Context, externalReference string) (*payment.SearchResponse, error) {
	cfg, err := mp.New(c.accessToken)
	if err != nil {
		return nil, fmt.Errorf("error creating mercadopago config: %w", err)
	}

	payClient := payment.NewClient(cfg)

	searchReq := payment.SearchRequest{
		Filters: map[string]string{
			"external_reference": externalReference,
		},
		Limit:  5,
		Offset: 0,
	}

	result, err := payClient.Search(ctx, searchReq)
	if err != nil {
		return nil, fmt.Errorf("error searching mercadopago payments by external_reference %s: %w", externalReference, err)
	}

	log.Printf("[MERCADOPAGO] Search by external_reference=%s: found %d results\n", externalReference, result.Paging.Total)
	return result, nil
}
