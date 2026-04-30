package infra

import (
	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/pkg/logger"
	"go.uber.org/zap"
)

type BaseHandler struct {
	logger *zap.SugaredLogger
}

func NewBaseHandler() *BaseHandler {
	return &BaseHandler{
		logger: logger.GetLogger(),
	}
}

func (h *BaseHandler) GetLogger(c *gin.Context) *zap.SugaredLogger {
	return logger.FromContext(c.Request.Context())
}

func (h *BaseHandler) GetUserID(c *gin.Context) string {
	if userID, exists := c.Get("user_id"); exists {
		if id, ok := userID.(string); ok {
			return id
		}
	}
	return logger.GetUserID(c.Request.Context())
}

func (h *BaseHandler) LogRequest(c *gin.Context, event string, fields ...interface{}) {
	log := h.GetLogger(c)
	log.Infow(event, h.buildFields(c, fields...)...)
}

func (h *BaseHandler) LogError(c *gin.Context, event string, err error, fields ...interface{}) {
	log := h.GetLogger(c)
	allFields := append(h.buildFields(c, fields...), "error", err)
	log.Errorw(event, allFields...)
}

func (h *BaseHandler) LogWarn(c *gin.Context, event string, err error, fields ...interface{}) {
	log := h.GetLogger(c)
	allFields := append(h.buildFields(c, fields...), "error", err)
	log.Warnw(event, allFields...)
}

func (h *BaseHandler) buildFields(c *gin.Context, fields ...interface{}) []interface{} {
	allFields := make([]interface{}, 0, len(fields)+4)
	
	allFields = append(allFields,
		"method", c.Request.Method,
		"path", c.Request.URL.Path,
		"trace_id", logger.GetTraceID(c.Request.Context()),
	)

	if userID := h.GetUserID(c); userID != "" {
		allFields = append(allFields, "user_id", userID)
	}

	allFields = append(allFields, fields...)

	return allFields
}
