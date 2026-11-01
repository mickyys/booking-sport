package infra

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hamp/booking-sport/internal/app"
)

type InboxHandler struct {
	useCase     *app.InboxUseCase
	baseHandler *BaseHandler
}

func NewInboxHandler(uc *app.InboxUseCase) *InboxHandler {
	return &InboxHandler{useCase: uc, baseHandler: NewBaseHandler()}
}

func (h *InboxHandler) userID(c *gin.Context) (string, bool) {
	id := h.baseHandler.GetUserID(c)
	if id == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found in token"})
		return "", false
	}
	return id, true
}

func (h *InboxHandler) fail(c *gin.Context, err error) {
	if errors.Is(err, app.ErrInboxMessageNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}

// List: GET /users/me/messages?before=<id>&limit=<n>
func (h *InboxHandler) List(c *gin.Context) {
	uid, ok := h.userID(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	msgs, err := h.useCase.List(c.Request.Context(), uid, c.Query("before"), limit)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": msgs})
}

func (h *InboxHandler) UnreadCount(c *gin.Context) {
	uid, ok := h.userID(c)
	if !ok {
		return
	}
	n, err := h.useCase.UnreadCount(c.Request.Context(), uid)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"unread": n})
}

func (h *InboxHandler) MarkRead(c *gin.Context) {
	uid, ok := h.userID(c)
	if !ok {
		return
	}
	if err := h.useCase.MarkRead(c.Request.Context(), uid, c.Param("id")); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *InboxHandler) MarkAllRead(c *gin.Context) {
	uid, ok := h.userID(c)
	if !ok {
		return
	}
	if err := h.useCase.MarkAllRead(c.Request.Context(), uid); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *InboxHandler) Delete(c *gin.Context) {
	uid, ok := h.userID(c)
	if !ok {
		return
	}
	if err := h.useCase.Delete(c.Request.Context(), uid, c.Param("id")); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
