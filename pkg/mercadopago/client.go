package mercadopago

import (
	"context"
	"fmt"
	"time"

	mp "github.com/mercadopago/sdk-go/pkg/config"
	"github.com/mercadopago/sdk-go/pkg/payment"
	"github.com/mercadopago/sdk-go/pkg/preference"
	"github.com/mercadopago/sdk-go/pkg/refund"

	"github.com/hamp/booking-sport/pkg/logger"
)

type RefundResult struct {
	ID     int
	Status string
	Amount float64
}

// PreferenceInput agrupa todos los parámetros necesarios para crear una
// preferencia de pago con split (marketplace).
type PreferenceInput struct {
	Title           string
	Amount          float64
	Email           string
	PayerName       string
	PayerSurname    string
	ExternalRef     string
	SuccessURL      string
	FailureURL      string
	PendingURL      string
	NotificationURL string
	ExpirationDate  *time.Time
	MarketplaceFee  float64
	Metadata        map[string]any
}

type Client struct {
	accessToken string
	config      *mp.Config
}

func NewClient(accessToken string) *Client {
	cfg, err := mp.New(accessToken)
	if err != nil {
		logger.GetLogger().Warnw("mercadopago_config_error",
			"error", err,
		)
	}
	return &Client{accessToken: accessToken, config: cfg}
}

// getConfig devuelve la configuración del cliente, recreándola solo si el primer
// intento falló (p.ej. token vacío al construirlo).
func (c *Client) getConfig() (*mp.Config, error) {
	if c.config != nil {
		return c.config, nil
	}
	cfg, err := mp.New(c.accessToken)
	if err != nil {
		return nil, fmt.Errorf("error creating mercadopago config: %w", err)
	}
	c.config = cfg
	return cfg, nil
}

type PreferenceResult struct {
	ID        string
	InitPoint string
}

// maskToken enmascara un access token mostrando solo los primeros y últimos
// caracteres. Es seguro para tokens vacíos o cortos.
func maskToken(token string) string {
	if len(token) <= 10 {
		return "***"
	}
	return token[:5] + "..." + token[len(token)-5:]
}

func (c *Client) CreatePreference(ctx context.Context, input PreferenceInput) (*PreferenceResult, error) {
	cfg, err := c.getConfig()
	if err != nil {
		return nil, err
	}

	prefClient := preference.NewClient(cfg)

	quantity := 1
	req := preference.Request{
		Items: []preference.ItemRequest{
			{
				Title:     input.Title,
				Quantity:  quantity,
				UnitPrice: input.Amount,
			},
		},
		Payer: &preference.PayerRequest{
			Email:   input.Email,
			Name:    input.PayerName,
			Surname: input.PayerSurname,
		},
		BackURLs: &preference.BackURLsRequest{
			Success: input.SuccessURL,
			Failure: input.FailureURL,
			Pending: input.PendingURL,
		},
		ExternalReference: input.ExternalRef,
		AutoReturn:        "approved",
		NotificationURL:   input.NotificationURL,
		ExpirationDateTo:  input.ExpirationDate,
		MarketplaceFee:    input.MarketplaceFee,
		Metadata:          input.Metadata,
	}

	log := logger.FromContext(ctx)
	log.Infow("mercadopago_preference_creating",
		"external_ref", input.ExternalRef,
		"amount", input.Amount,
		"marketplace_fee", req.MarketplaceFee,
		"token", maskToken(c.accessToken),
	)

	result, err := prefClient.Create(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("error creating mercadopago preference: %w", err)
	}

	log.Infow("mercadopago_preference_created",
		"preference_id", result.ID,
		"init_point", result.InitPoint,
		"token", maskToken(c.accessToken),
	)

	return &PreferenceResult{
		ID:        result.ID,
		InitPoint: result.InitPoint,
	}, nil
}

func (c *Client) GetPayment(ctx context.Context, paymentID int) (*payment.Response, error) {
	cfg, err := c.getConfig()
	if err != nil {
		return nil, err
	}

	payClient := payment.NewClient(cfg)

	result, err := payClient.Get(ctx, paymentID)
	if err != nil {
		return nil, fmt.Errorf("error getting mercadopago payment: %w", err)
	}

	logger.FromContext(ctx).Infow("mercadopago_payment_status",
		"payment_id", paymentID,
		"status", result.Status,
	)
	return result, nil
}

// CreateRefund crea un reembolso total para un pago
func (c *Client) CreateRefund(ctx context.Context, paymentID int) (*RefundResult, error) {
	log := logger.FromContext(ctx)
	log.Infow("mercadopago_refund_creating",
		"payment_id", paymentID,
		"token", maskToken(c.accessToken),
	)
	cfg, err := c.getConfig()
	if err != nil {
		return nil, err
	}

	refundClient := refund.NewClient(cfg)
	result, err := refundClient.Create(ctx, paymentID)
	if err != nil {
		return nil, fmt.Errorf("error creating mercadopago refund for payment %d: %w", paymentID, err)
	}

	log.Infow("mercadopago_refund_created",
		"refund_id", result.ID,
		"payment_id", paymentID,
		"amount", result.Amount,
		"status", result.Status,
	)

	return &RefundResult{
		ID:     result.ID,
		Status: result.Status,
		Amount: result.Amount,
	}, nil
}

// CreatePartialRefund crea un reembolso parcial para un pago
func (c *Client) CreatePartialRefund(ctx context.Context, paymentID int, amount float64) (*RefundResult, error) {
	cfg, err := c.getConfig()
	if err != nil {
		return nil, err
	}

	refundClient := refund.NewClient(cfg)
	result, err := refundClient.CreatePartialRefund(ctx, paymentID, amount)
	if err != nil {
		return nil, fmt.Errorf("error creating partial mercadopago refund for payment %d: %w", paymentID, err)
	}

	logger.FromContext(ctx).Infow("mercadopago_partial_refund_created",
		"refund_id", result.ID,
		"payment_id", paymentID,
		"amount", result.Amount,
		"status", result.Status,
	)

	return &RefundResult{
		ID:     result.ID,
		Status: result.Status,
		Amount: result.Amount,
	}, nil
}

func (c *Client) SearchPaymentsByExternalReference(ctx context.Context, externalReference string) (*payment.SearchResponse, error) {
	cfg, err := c.getConfig()
	if err != nil {
		return nil, err
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

	logger.FromContext(ctx).Infow("mercadopago_payment_search",
		"external_reference", externalReference,
		"total", result.Paging.Total,
	)
	return result, nil
}
