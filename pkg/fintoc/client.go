package fintoc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type CheckoutSessionRequest struct {
	Amount        int               `json:"amount"`
	Currency      string            `json:"currency"`
	CustomerEmail string            `json:"customer_email"`
	SuccessURL    string            `json:"success_url"`
	CancelURL     string            `json:"cancel_url"`
	Metadata      map[string]string `json:"metadata"`
}

type CheckoutSessionResponse struct {
	ID           string `json:"id"`
	SessionToken string `json:"session_token"`
	Status       string `json:"status"`
	RedirectURL  string `json:"redirect_url"`
}

type Client struct {
	SecretKey string
	BaseURL   string
}

func NewClient(secretKey string) *Client {
	return &Client{
		SecretKey: secretKey,
		BaseURL:   "https://api.fintoc.com/v1",
	}
}

func (c *Client) CreateCheckoutSession(amount int, currency string, email string, orderID string, successURL string, cancelURL string) (*CheckoutSessionResponse, error) {
	reqBody := CheckoutSessionRequest{
		Amount:        amount,
		Currency:      currency,
		CustomerEmail: email,
		SuccessURL:    successURL,
		CancelURL:     cancelURL,
		Metadata: map[string]string{
			"order_id": orderID,
		},
	}

	jsonBody, _ := json.Marshal(reqBody)
	fmt.Printf("[FINTOC DEBUG] Creando Checkout Session: %s\n", string(jsonBody))

	req, err := http.NewRequest("POST", c.BaseURL+"/checkout_sessions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", c.SecretKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		fmt.Printf("[FINTOC ERROR] Código: %d, Cuerpo: %s\n", resp.StatusCode, string(respBody))
		return nil, fmt.Errorf("fintoc error: %s", resp.Status)
	}

	var result CheckoutSessionResponse
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("[FINTOC DEBUG] Respuesta de Fintoc: %s\n", string(body))
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

type CheckoutSessionDetailResponse struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	PaymentResource struct {
		PaymentIntent struct {
			ID string `json:"id"`
		} `json:"payment_intent"`
	} `json:"payment_resource"`
}

func (c *Client) GetCheckoutSession(id string) (*CheckoutSessionDetailResponse, error) {
	req, err := http.NewRequest("GET", c.BaseURL+"/checkout_sessions/"+id, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", c.SecretKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fintoc error: %d - %s", resp.StatusCode, string(respBody))
	}

	var result CheckoutSessionDetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

type PaymentIntentResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Amount int    `json:"amount"`
}

func (c *Client) GetPaymentIntent(id string) (*PaymentIntentResponse, error) {
	req, err := http.NewRequest("GET", c.BaseURL+"/payment_intents/"+id, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", c.SecretKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fintoc error: %d - %s", resp.StatusCode, string(respBody))
	}

	var result PaymentIntentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}
