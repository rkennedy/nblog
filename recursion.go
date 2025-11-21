package nblog

import (
	"io"
	"log/slog"
)

func baseHandler(h *Handler) *Handler {
	for h.previousHandler != nil {
		h = h.previousHandler
	}
	return h
}

func getLevel(h *Handler) slog.Leveler {
	return baseHandler(h).level
}

func destination(h *Handler) io.Writer {
	return baseHandler(h).destination
}

func identityAttribute(_ []string, a slog.Attr) slog.Attr {
	return a
}

func getReplaceAttr(h *Handler) ReplaceAttrFunc {
	result := baseHandler(h).replaceAttr
	if result == nil {
		return identityAttribute
	}
	return result
}

func getTimestampFormat(h *Handler) string {
	return baseHandler(h).timestampFormat
}

func getUseFullCallerName(h *Handler) bool {
	return baseHandler(h).useFullCallerName
}

func getNumericSeverity(h *Handler) bool {
	return baseHandler(h).numericSeverity
}

func (h *Handler) groups() []string {
	if h.previousHandler == nil {
		return []string{}
	}
	result := h.previousHandler.groups()
	if h.group != "" {
		return append(result, h.group)
	}
	return result
}
