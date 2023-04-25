package nblog

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	jsoniter "github.com/json-iterator/go"

	"golang.org/x/exp/slog"
)

// LegacyHandler is an implementation of [golang.org/x/exp/slog.Handler] that
// mimics the format used by legacy NetBackup logging. Attributes, if present,
// are appended to the line after the given message.
//
// If an attribute named “who” is present, it overrides the name of the
// calling function.
type LegacyHandler struct {
	Destination io.Writer
	Level       slog.Leveler
	// TimestampFormat is the format (a la time.Time.Format) to use for
	// timestamps at the start of each log record. If empty, the default
	// used will be FullDateFormat. The classic NetBackup format is
	// TimeOnlyFormat; use that if log rotation would make the repeated
	// inclusion of the date redundant.
	TimestampFormat string
	attrs           []slog.Attr
}

var _ slog.Handler = &LegacyHandler{}

type LegacyOption func(handler *LegacyHandler)

func Level(level slog.Leveler) LegacyOption {
	return func(handler *LegacyHandler) {
		handler.Level = level
	}
}

const (
	FullDateFormat = time.DateTime + ".000"
	TimeOnlyFormat = time.TimeOnly + ".000"
)

// TimestampFormat returns a LegacyOption callback that will configure a
// handler to use the given time format for log timestamps. See
// LegacyHandler.TimestampFormat.
func TimestampFormat(format string) LegacyOption {
	return func(handler *LegacyHandler) {
		handler.TimestampFormat = format
	}
}

func NewHandler(dest io.Writer, options ...LegacyOption) *LegacyHandler {
	result := &LegacyHandler{
		Destination:     dest,
		Level:           slog.LevelInfo,
		TimestampFormat: FullDateFormat,
	}
	for _, opt := range options {
		opt(result)
	}
	return result
}

func (h *LegacyHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Level.Level() <= level
}

func nextField(wroteFirstSpace *bool, firstField *bool, out *jsoniter.Stream, key string) {
	if wroteFirstSpace != nil && !*wroteFirstSpace {
		*wroteFirstSpace = true
		out.WriteRaw(" ")
		out.WriteObjectStart()
	} else if !*firstField {
		out.WriteMore()
		out.WriteRaw(" ")
	}
	*firstField = false
	out.WriteObjectField(key)
	out.WriteRaw(" ")
}

func attrToJson(wroteFirstSpace *bool, firstField *bool, out *jsoniter.Stream, attr slog.Attr) {
	switch attr.Value.Kind() {
	case slog.KindString:
		nextField(wroteFirstSpace, firstField, out, attr.Key)
		out.WriteString(attr.Value.String())
	case slog.KindInt64:
		nextField(wroteFirstSpace, firstField, out, attr.Key)
		out.WriteInt64(attr.Value.Int64())
	case slog.KindUint64:
		nextField(wroteFirstSpace, firstField, out, attr.Key)
		out.WriteUint64(attr.Value.Uint64())
	case slog.KindFloat64:
		nextField(wroteFirstSpace, firstField, out, attr.Key)
		out.WriteFloat64(attr.Value.Float64())
	case slog.KindBool:
		nextField(wroteFirstSpace, firstField, out, attr.Key)
		out.WriteBool(attr.Value.Bool())
	case slog.KindDuration:
		nextField(wroteFirstSpace, firstField, out, attr.Key)
		// TODO attr.Value.Duration()
	case slog.KindTime:
		nextField(wroteFirstSpace, firstField, out, attr.Key)
		// TODO attr.Value.Time()
	case slog.KindAny:
		nextField(wroteFirstSpace, firstField, out, attr.Key)
		out.WriteVal(attr.Value.Any())
	case slog.KindGroup:
		nextField(wroteFirstSpace, firstField, out, attr.Key)
		out.WriteObjectStart()
		thisFirst := true
		for _, at := range attr.Value.Group() {
			attrToJson(nil, &thisFirst, out, at)
		}
		out.WriteObjectEnd()
	}
}

func (h *LegacyHandler) Handle(ctx context.Context, rec slog.Record) error {
	if !rec.Time.IsZero() {
		format := h.TimestampFormat
		if format == "" {
			format = FullDateFormat
		}
		fmt.Fprintf(h.Destination, "%s ", rec.Time.Format(format))
	}
	frames := runtime.CallersFrames([]uintptr{rec.PC})
	frame, _ := frames.Next()
	who := frame.Function
	fmt.Fprintf(h.Destination, "[%d] <%s> %s: %s", os.Getpid(), rec.Level, who, rec.Message)
	out := jsoniter.NewStream(jsoniter.ConfigDefault, h.Destination, 50)
	wroteSpace := false
	firstField := true
	rec.Attrs(func(attr slog.Attr) bool {
		attrToJson(&wroteSpace, &firstField, out, attr)
		return true
	})
	if wroteSpace {
		out.WriteObjectEnd()
		out.Flush()
	}
	fmt.Fprint(h.Destination, "\n")
	return nil
}

func (h *LegacyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LegacyHandler{
		Destination: h.Destination,
		Level:       h.Level,
		attrs:       append(h.attrs, attrs...),
	}
}

func (h *LegacyHandler) WithGroup(name string) slog.Handler {
	// TODO Implement log groups
	return h
}
