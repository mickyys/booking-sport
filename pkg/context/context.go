package contextutil

import (
	"context"

	"github.com/hamp/booking-sport/pkg/logger"
)

type ContextBuilder struct {
	traceID string
	spanID  string
	userID  string
}

func NewContextBuilder() *ContextBuilder {
	return &ContextBuilder{}
}

func (b *ContextBuilder) WithTraceID(traceID string) *ContextBuilder {
	b.traceID = traceID
	return b
}

func (b *ContextBuilder) WithSpanID(spanID string) *ContextBuilder {
	b.spanID = spanID
	return b
}

func (b *ContextBuilder) WithUserID(userID string) *ContextBuilder {
	b.userID = userID
	return b
}

func (b *ContextBuilder) Build(ctx context.Context) context.Context {
	if b.traceID != "" {
		ctx = logger.WithTraceID(ctx, b.traceID)
	}
	if b.spanID != "" {
		ctx = logger.WithSpanID(ctx, b.spanID)
	}
	if b.userID != "" {
		ctx = logger.WithUserID(ctx, b.userID)
	}
	return ctx
}

func ExtractUserInfo(ctx context.Context) (traceID, spanID, userID string) {
	return logger.GetTraceID(ctx), logger.GetSpanID(ctx), logger.GetUserID(ctx)
}

func CloneWithContext(ctx context.Context) context.Context {
	traceID, spanID, userID := ExtractUserInfo(ctx)
	return NewContextBuilder().
		WithTraceID(traceID).
		WithSpanID(spanID).
		WithUserID(userID).
		Build(context.Background())
}
