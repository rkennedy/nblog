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

func level(h *Handler) slog.Leveler {
	return baseHandler(h).level
}

func destination(h *Handler) io.Writer {
	return baseHandler(h).destination
}

func replaceAttr(h *Handler) ReplaceAttrFunc {
	return baseHandler(h).replaceAttr
}

func timestampFormat(h *Handler) string {
	return baseHandler(h).timestampFormat
}

func useFullCallerName(h *Handler) bool {
	return baseHandler(h).useFullCallerName
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
