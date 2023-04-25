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

type Options struct {
	Level slog.Leveler
}

type Handler struct {
	Destination io.Writer
	Level       slog.Leveler
	attrs       []slog.Attr
}

var _ slog.Handler = &Handler{}

func NewHandler(dest io.Writer, options Options) *Handler {
	return &Handler{
		Destination: dest,
		Level:       options.Level,
	}
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Level.Level() <= level
}

const timestampFormat = time.DateTime + ".000"

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

func (h *Handler) Handle(ctx context.Context, rec slog.Record) error {
	if !rec.Time.IsZero() {
		fmt.Fprintf(h.Destination, "%s ", rec.Time.Format(timestampFormat))
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

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{
		Destination: h.Destination,
		Level:       h.Level,
		attrs:       append(h.attrs, attrs...),
	}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	// TODO Implement log groups
	return h
}
