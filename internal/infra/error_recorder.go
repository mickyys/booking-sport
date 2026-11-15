package infra

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/domain"
	"github.com/hamp/booking-sport/pkg/logger"
)

// ErrorLogStore es lo que el recorder necesita del repositorio.
type ErrorLogStore interface {
	Insert(ctx context.Context, l *domain.BookingErrorLog) (bool, error)
}

// ErrorContext aporta los datos de la reserva y del cliente que el handler ya conoce.
type ErrorContext struct {
	BookingID     string
	BookingCode   string
	SportCenterID string
	CourtID       string
	MPPaymentID   string
	UserID        string
	UserEmail     string
	UserName      string
	UserPhone     string
	Provider      string
}

// ErrorRecorder guarda errores de pago y reserva. Todos sus métodos son
// seguros con receptor nil: sin repositorio no hacen nada.
type ErrorRecorder struct {
	store ErrorLogStore
}

func NewErrorRecorder(store ErrorLogStore) *ErrorRecorder {
	return &ErrorRecorder{store: store}
}

const (
	maxErrorMessageLen  = 4096
	trackingIDAlphabet  = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	trackingIDLength    = 8
	errorRecordTimeout  = 3 * time.Second
	errorRecordAttempts = 3
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(APP_USR|TEST)-[A-Za-z0-9_\-]{10,}`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]+`),
	regexp.MustCompile(`(?i)(access_token|refresh_token|client_secret|authorization|api[_-]?key|password)(["']?\s*[:=]\s*["']?)[^\s"'&,}]+`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`),
}

// sanitizeErrorText oculta credenciales y limita el largo.
func sanitizeErrorText(s string) string {
	s = secretPatterns[0].ReplaceAllString(s, "[redacted]")
	s = secretPatterns[1].ReplaceAllString(s, "Bearer [redacted]")
	s = secretPatterns[2].ReplaceAllString(s, "${1}${2}[redacted]")
	s = secretPatterns[3].ReplaceAllString(s, "[redacted]")
	if len(s) > maxErrorMessageLen {
		s = s[:maxErrorMessageLen] + "…"
	}
	return s
}

func newTrackingID() string {
	b := make([]byte, trackingIDLength)
	_, _ = rand.Read(b)
	var sb strings.Builder
	sb.WriteString("ERR-")
	for _, v := range b {
		sb.WriteByte(trackingIDAlphabet[int(v)%len(trackingIDAlphabet)])
	}
	return sb.String()
}

// Record guarda el error y devuelve el tracking_id para mostrarlo al usuario.
// Nunca falla la request: ante un error de Mongo devuelve "".
func (r *ErrorRecorder) Record(c *gin.Context, operation string, err error, status int, ec ErrorContext) string {
	if r == nil || r.store == nil || err == nil {
		return ""
	}
	entry := &domain.BookingErrorLog{
		TraceID:          logger.GetTraceID(c.Request.Context()),
		Operation:        operation,
		Message:          sanitizeErrorText(err.Error()),
		HTTPStatus:       status,
		UserID:           ec.UserID,
		UserEmail:        ec.UserEmail,
		UserName:         ec.UserName,
		UserPhone:        ec.UserPhone,
		BookingID:        ec.BookingID,
		BookingCode:      ec.BookingCode,
		SportCenterID:    ec.SportCenterID,
		CourtID:          ec.CourtID,
		MPPaymentID:      ec.MPPaymentID,
		ProviderResponse: sanitizeErrorText(ec.Provider),
		Method:           c.Request.Method,
		Path:             c.Request.URL.Path,
		UserAgent:        truncate(c.Request.UserAgent(), 300),
		IP:               c.ClientIP(),
		Client:           c.GetHeader("X-Client"),
	}
	if entry.UserID == "" {
		if v, ok := c.Get("user_id"); ok {
			entry.UserID, _ = v.(string)
		}
	}
	if entry.UserEmail == "" {
		if v, ok := c.Get("user_email"); ok {
			entry.UserEmail, _ = v.(string)
		}
	}
	return r.insert(entry)
}

// RecordBackground registra errores sin request (webhooks procesados aparte).
func (r *ErrorRecorder) RecordBackground(traceID, operation string, err error, ec ErrorContext) string {
	if r == nil || r.store == nil || err == nil {
		return ""
	}
	return r.insert(&domain.BookingErrorLog{
		TraceID:          traceID,
		Operation:        operation,
		Message:          sanitizeErrorText(err.Error()),
		HTTPStatus:       http.StatusInternalServerError,
		MPPaymentID:      ec.MPPaymentID,
		BookingID:        ec.BookingID,
		BookingCode:      ec.BookingCode,
		ProviderResponse: sanitizeErrorText(ec.Provider),
	})
}

func (r *ErrorRecorder) insert(entry *domain.BookingErrorLog) string {
	ctx, cancel := context.WithTimeout(context.Background(), errorRecordTimeout)
	defer cancel()
	entry.CreatedAt = time.Now()
	for i := 0; i < errorRecordAttempts; i++ {
		entry.TrackingID = newTrackingID()
		dup, insErr := r.store.Insert(ctx, entry)
		if insErr != nil {
			logger.GetLogger().Warnw("error_log_insert_failed", "error", insErr)
			return ""
		}
		if !dup {
			return entry.TrackingID
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// errorBody arma el JSON de error, con tracking_id si se pudo registrar y los
// detalles estructurados del AppError (por ejemplo, lock_expires_at).
func errorBody(err error, trackingID string) gin.H {
	body := gin.H{"error": err.Error()}
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		for k, v := range appErr.Details {
			body[k] = v
		}
	}
	if trackingID != "" {
		body["tracking_id"] = trackingID
	}
	return body
}
