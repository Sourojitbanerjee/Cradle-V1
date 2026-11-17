package telemetry

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// RedactingHandler wraps an slog.Handler to sanitize sensitive keys and values before writing.
type RedactingHandler struct {
	inner slog.Handler
}

// NewRedactingHandler creates an slog.Handler with active credential redaction.
func NewRedactingHandler(inner slog.Handler) *RedactingHandler {
	return &RedactingHandler{inner: inner}
}

func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *RedactingHandler) Handle(ctx context.Context, r slog.Record) error {
	redactedRecord := slog.NewRecord(r.Time, r.Level, RedactString(r.Message), r.PC)

	r.Attrs(func(a slog.Attr) bool {
		redactedRecord.AddAttrs(sanitizeAttr(a))
		return true
	})

	return h.inner.Handle(ctx, redactedRecord)
}

func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	sanitized := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		sanitized[i] = sanitizeAttr(a)
	}
	return &RedactingHandler{inner: h.inner.WithAttrs(sanitized)}
}

func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{inner: h.inner.WithGroup(name)}
}

func sanitizeAttr(a slog.Attr) slog.Attr {
	if IsSensitiveKey(a.Key) {
		return slog.String(a.Key, MaskedReplacement)
	}

	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, RedactString(a.Value.String()))
	case slog.KindGroup:
		groupAttrs := a.Value.Group()
		sanitizedGroup := make([]slog.Attr, len(groupAttrs))
		for i, ga := range groupAttrs {
			sanitizedGroup[i] = sanitizeAttr(ga)
		}
		return slog.Group(a.Key, anySlice(sanitizedGroup)...)
	default:
		return a
	}
}

func anySlice(attrs []slog.Attr) []any {
	anys := make([]any, len(attrs))
	for i, a := range attrs {
		anys[i] = a
	}
	return anys
}

// NewLogger creates a production-grade structured JSON logger with secret redaction.
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}
	baseHandler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(NewRedactingHandler(baseHandler))
}
