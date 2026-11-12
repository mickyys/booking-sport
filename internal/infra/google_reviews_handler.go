package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
)

const (
	googleReviewsTTL      = 24 * time.Hour
	googleReviewsErrorTTL = 5 * time.Minute
	googlePlacesBaseURL   = "https://places.googleapis.com/v1/places/"
	googlePlacesFieldMask = "rating,userRatingCount,googleMapsUri,reviews"
	maxGoogleReviews      = 5
)

type GoogleReview struct {
	Author       string  `json:"author"`
	AuthorPhoto  string  `json:"author_photo,omitempty"`
	AuthorURL    string  `json:"author_url,omitempty"`
	Rating       float64 `json:"rating"`
	Text         string  `json:"text"`
	RelativeTime string  `json:"relative_time,omitempty"`
	PublishTime  string  `json:"publish_time,omitempty"`
}

type GoogleReviewsSummary struct {
	Rating       float64        `json:"rating"`
	TotalReviews int            `json:"total_reviews"`
	MapsURL      string         `json:"maps_url,omitempty"`
	Reviews      []GoogleReview `json:"reviews"`
}

type googleReviewsCacheEntry struct {
	summary   *GoogleReviewsSummary
	expiresAt time.Time
}

// GoogleReviewsHandler consulta Google Places (New) desde el backend para no
// exponer la API key y cachea el resultado por Place ID.
type GoogleReviewsHandler struct {
	useCase *app.SportCenterUseCase
	apiKey  string
	client  *http.Client

	mu    sync.Mutex
	cache map[string]googleReviewsCacheEntry
}

func NewGoogleReviewsHandler(uc *app.SportCenterUseCase) *GoogleReviewsHandler {
	return &GoogleReviewsHandler{
		useCase: uc,
		apiKey:  strings.TrimSpace(os.Getenv("GOOGLE_PLACES_API_KEY")),
		client:  &http.Client{Timeout: 6 * time.Second},
		cache:   map[string]googleReviewsCacheEntry{},
	}
}

// GetBySlug responde con la calificación y reseñas de Google del centro.
// Si el centro no tiene Place ID o la integración no está configurada
// devuelve 200 con "available": false para que la UI oculte la sección.
func (h *GoogleReviewsHandler) GetBySlug(c *gin.Context) {
	center, err := h.useCase.FindBySlug(c.Request.Context(), c.Param("slug"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Sport center not found"})
		return
	}
	if center.GooglePlaceID == "" || h.apiKey == "" {
		c.JSON(http.StatusOK, gin.H{"available": false})
		return
	}

	summary, err := h.load(c.Request.Context(), center.GooglePlaceID)
	if err != nil || summary == nil {
		if err != nil {
			log.Printf("google reviews: slug=%s: %v", center.Slug, err)
		}
		c.JSON(http.StatusOK, gin.H{"available": false})
		return
	}

	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, gin.H{"available": true, "google": summary})
}

func (h *GoogleReviewsHandler) load(ctx context.Context, placeID string) (*GoogleReviewsSummary, error) {
	h.mu.Lock()
	if e, ok := h.cache[placeID]; ok && time.Now().Before(e.expiresAt) {
		h.mu.Unlock()
		return e.summary, nil
	}
	h.mu.Unlock()

	summary, err := h.fetch(ctx, placeID)
	ttl := googleReviewsTTL
	if err != nil {
		ttl = googleReviewsErrorTTL
	}

	h.mu.Lock()
	h.cache[placeID] = googleReviewsCacheEntry{summary: summary, expiresAt: time.Now().Add(ttl)}
	h.mu.Unlock()
	return summary, err
}

func (h *GoogleReviewsHandler) fetch(ctx context.Context, placeID string) (*GoogleReviewsSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googlePlacesBaseURL+url.PathEscape(placeID)+"?languageCode=es", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Goog-Api-Key", h.apiKey)
	req.Header.Set("X-Goog-FieldMask", googlePlacesFieldMask)

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("places api status %d", resp.StatusCode)
	}

	var payload struct {
		Rating          float64 `json:"rating"`
		UserRatingCount int     `json:"userRatingCount"`
		GoogleMapsURI   string  `json:"googleMapsUri"`
		Reviews         []struct {
			Rating                         float64 `json:"rating"`
			RelativePublishTimeDescription string  `json:"relativePublishTimeDescription"`
			PublishTime                    string  `json:"publishTime"`
			Text                           struct {
				Text string `json:"text"`
			} `json:"text"`
			AuthorAttribution struct {
				DisplayName string `json:"displayName"`
				URI         string `json:"uri"`
				PhotoURI    string `json:"photoUri"`
			} `json:"authorAttribution"`
		} `json:"reviews"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	summary := &GoogleReviewsSummary{
		Rating:       payload.Rating,
		TotalReviews: payload.UserRatingCount,
		MapsURL:      payload.GoogleMapsURI,
		Reviews:      []GoogleReview{},
	}
	for _, r := range payload.Reviews {
		if len(summary.Reviews) >= maxGoogleReviews {
			break
		}
		summary.Reviews = append(summary.Reviews, GoogleReview{
			Author:       r.AuthorAttribution.DisplayName,
			AuthorPhoto:  r.AuthorAttribution.PhotoURI,
			AuthorURL:    r.AuthorAttribution.URI,
			Rating:       r.Rating,
			Text:         r.Text.Text,
			RelativeTime: r.RelativePublishTimeDescription,
			PublishTime:  r.PublishTime,
		})
	}
	return summary, nil
}
