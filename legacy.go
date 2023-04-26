package nblog

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/rkennedy/optional"

	"golang.org/x/exp/slog"
)

// LegacyHandler is an implementation of [golang.org/x/exp/slog.Handler] that
// mimics the format used by legacy NetBackup logging. Attributes, if present,
// are appended to the line after the given message.
//
// If an attribute named “who” is present, it overrides the name of the
// calling function.
type LegacyHandler struct {
	destination       io.Writer
	level             slog.Leveler
	timestampFormat   optional.Value[string]
	useFullCallerName bool
	attrs             []slog.Attr
}

var _ slog.Handler = &LegacyHandler{}

// LegacyOption is a function that applies options to a new [LegacyHandler].
// Pass instances of this function to [NewHandler]. Use functions like
// [TimestampFormat] to generate callbacks to apply variable values.
type LegacyOption func(handler *LegacyHandler)

// Level returns a LegacyOption callback that will configure a handler to
// filter messages with a level lower than the given level.
func Level(level slog.Leveler) LegacyOption {
	return func(handler *LegacyHandler) {
		handler.level = level
	}
}

const (
	FullDateFormat = time.DateTime + ".000"
	TimeOnlyFormat = time.TimeOnly + ".000"
)

// TimestampFormat returns a LegacyOption callback that will configure a
// handler to use the given time format (a la [time.Time.Format]) for log
// timestamps at the start of each record. If left unset, the default used will
// be [FullDateFormat]. The classic NetBackup format is [TimeOnlyFormat]; use
// that if log rotation would make the repeated inclusion of the date
// redundant.
func TimestampFormat(format string) LegacyOption {
	return func(handler *LegacyHandler) {
		handler.timestampFormat = optional.New(format)
	}
}

// UseFullCallerName returns a LegacyOption callback that will configure a
// handler to include or omit the package-name portion of the caller in log
// messages. The default is to omit the package, so only the function name will
// appear.
func UseFullCallerName(use bool) LegacyOption {
	return func(handler *LegacyHandler) {
		handler.useFullCallerName = use
	}
}

func NewHandler(dest io.Writer, options ...LegacyOption) *LegacyHandler {
	result := &LegacyHandler{
		destination:       dest,
		level:             slog.LevelInfo,
		useFullCallerName: false,
	}
	for _, opt := range options {
		opt(result)
	}
	return result
}

func (h *LegacyHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.level.Level() <= level
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
		out.WriteString(attr.Value.Duration().String())
	case slog.KindTime:
		nextField(wroteFirstSpace, firstField, out, attr.Key)
		out.WriteString(attr.Value.Time().String())
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

func getCaller(rec slog.Record, omitPackage bool) string {
	frames := runtime.CallersFrames([]uintptr{rec.PC})
	frame, _ := frames.Next()
	who := frame.Function
	if omitPackage {
		lastDot := strings.LastIndex(who, ".")
		if lastDot >= 0 {
			who = who[lastDot+1:]
		}
	}
	return who
}

func (h *LegacyHandler) Handle(ctx context.Context, rec slog.Record) error {
	var timeString string
	if !rec.Time.IsZero() {
		format := h.timestampFormat.OrElse(FullDateFormat)
		timeString = fmt.Sprintf("%s ", rec.Time.Format(format))
	}

	attributeBuffer := &strings.Builder{}
	out := jsoniter.NewStream(jsoniter.ConfigDefault, attributeBuffer, 50)
	wroteSpace := false
	firstField := true
	var who optional.Value[string]

	processAttr := func(attr slog.Attr) bool {
		if attr.Key == "who" {
			who = optional.New(attr.Value.String())
		} else {
			attrToJson(&wroteSpace, &firstField, out, attr)
		}
		return true
	}
	for _, attr := range h.attrs {
		processAttr(attr)
	}
	rec.Attrs(processAttr)
	if wroteSpace {
		out.WriteObjectEnd()
		out.Flush()
	}

	fmt.Fprintf(h.destination, "%s[%d] <%s> %s: %s%s\n",
		timeString,
		os.Getpid(),
		rec.Level,
		who.OrElseGet(func() string { return getCaller(rec, !h.useFullCallerName) }),
		rec.Message,
		attributeBuffer.String(),
	)
	return nil
}

func (h *LegacyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LegacyHandler{
		destination: h.destination,
		level:       h.level,
		attrs:       append(h.attrs, attrs...),
	}
}

func (h *LegacyHandler) WithGroup(name string) slog.Handler {
	// TODO Implement log groups
	return h
}
