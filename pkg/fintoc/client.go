package fintoc

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

func VerifySignature(payload []byte, signatureHeader string, secret string) bool {

	parts := strings.Split(signatureHeader, ",")
	var t, v1 string
	for _, part := range parts {
		kv := strings.Split(part, "=")
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		if key == "t" {
			t = value
		} else if key == "v1" {
			v1 = value
		}
	}

	if t == "" || v1 == "" {
		log.Printf("[FINTOC] Missing t or v1 in signature header")
		return false
	}

	// 2. Re-building the signed message
	// Re-build the message using: {timestamp}.{raw_body}
	message := t + "." + string(payload)

	// 3. Generating the signature
	// Using SHA-256 and the secret
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	generatedSignature := hex.EncodeToString(mac.Sum(nil))

	// 4. Comparing the signatures
	// Use hmac.Equal for a constant-time comparison

	log.Println("v1:", v1)
	log.Println("Generated Signature:", generatedSignature)

	isValid := hmac.Equal([]byte(v1), []byte(generatedSignature))

	if !isValid {
		log.Printf("[DEBUG FINTOC] Signature Mismatch!")
		log.Printf("Timestamp (t): %s", t)
		log.Printf("Received Signature (v1): %s", v1)
		log.Printf("Expected Signature: %s", generatedSignature)
		// Opcional: Log del message construido para depurar discrepancias de body
		// log.Printf("Signed Message: %s", message)
	} else {
		log.Printf("[DEBUG FINTOC] Signature Verified Successfully for t=%s", t)
	}

	return isValid
}

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
	baseURL := os.Getenv("FINTOC_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.fintoc.com/v1"
	}
	return &Client{
		SecretKey: secretKey,
		BaseURL:   baseURL,
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
