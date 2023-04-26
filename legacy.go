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
	who               optional.Value[string]

	attrs      *strings.Builder
	needComma  bool
	braceLevel uint
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
		attrs:             &strings.Builder{},
	}
	for _, opt := range options {
		opt(result)
	}
	return result
}

func (h *LegacyHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.level.Level() <= level
}

func nextField(firstField *bool, out *jsoniter.Stream, key string) {
	if !*firstField {
		out.WriteMore()
		out.WriteRaw(" ")
	}
	*firstField = false
	out.WriteObjectField(key)
	out.WriteRaw(" ")
}

func attrToJson(firstField *bool, out *jsoniter.Stream, attr slog.Attr) {
	switch attr.Value.Kind() {
	case slog.KindString:
		nextField(firstField, out, attr.Key)
		out.WriteString(attr.Value.String())
	case slog.KindInt64:
		nextField(firstField, out, attr.Key)
		out.WriteInt64(attr.Value.Int64())
	case slog.KindUint64:
		nextField(firstField, out, attr.Key)
		out.WriteUint64(attr.Value.Uint64())
	case slog.KindFloat64:
		nextField(firstField, out, attr.Key)
		out.WriteFloat64(attr.Value.Float64())
	case slog.KindBool:
		nextField(firstField, out, attr.Key)
		out.WriteBool(attr.Value.Bool())
	case slog.KindDuration:
		nextField(firstField, out, attr.Key)
		out.WriteString(attr.Value.Duration().String())
	case slog.KindTime:
		nextField(firstField, out, attr.Key)
		out.WriteString(attr.Value.Time().String())
	case slog.KindAny:
		nextField(firstField, out, attr.Key)
		out.WriteVal(attr.Value.Any())
	case slog.KindGroup:
		nextField(firstField, out, attr.Key)
		out.WriteObjectStart()
		thisFirst := true
		for _, at := range attr.Value.Group() {
			attrToJson(&thisFirst, out, at)
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

func cloneBuilder(b *strings.Builder) *strings.Builder {
	result := &strings.Builder{}
	_, _ = result.WriteString(b.String())
	return result
}

func (h *LegacyHandler) Handle(ctx context.Context, rec slog.Record) error {
	var timeString string
	if !rec.Time.IsZero() {
		format := h.timestampFormat.OrElse(FullDateFormat)
		timeString = fmt.Sprintf("%s ", rec.Time.Format(format))
	}

	who := h.who
	immediateAttrs := 0
	rec.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "who" {
			who = optional.New(attr.Value.String())
			return true
		}
		if attr.Value.Kind() == slog.KindGroup && len(attr.Value.Group()) == 0 {
			// JSONHandler omits empty group attributes, so we will, too.
			return true
		}
		immediateAttrs++
		return true
	})

	attributeBuffer := cloneBuilder(h.attrs)
	out := jsoniter.NewStream(jsoniter.ConfigDefault, attributeBuffer, 50)
	braceLevel := h.braceLevel
	needComma := h.needComma
	if immediateAttrs > 0 && h.attrs.Len() == 0 {
		out.WriteRaw(" ")
		out.WriteObjectStart()
		braceLevel++
		needComma = false
	}

	rec.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "who" {
			return true
		}
		if attr.Value.Kind() == slog.KindGroup && len(attr.Value.Group()) == 0 {
			// JSONHandler omits empty group attributes, so we will, too.
			return true
		}
		if needComma {
			out.WriteMore()
			out.WriteRaw(" ")
		}
		switch attr.Value.Kind() {
		case slog.KindString:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteString(attr.Value.String())
		case slog.KindInt64:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteInt64(attr.Value.Int64())
		case slog.KindUint64:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteUint64(attr.Value.Uint64())
		case slog.KindFloat64:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteFloat64(attr.Value.Float64())
		case slog.KindBool:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteBool(attr.Value.Bool())
		case slog.KindDuration:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteString(attr.Value.Duration().String())
		case slog.KindTime:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteString(attr.Value.Time().String())
		case slog.KindAny:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteVal(attr.Value.Any())
		case slog.KindGroup:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteObjectStart()
			thisFirst := true
			for _, at := range attr.Value.Group() {
				attrToJson(&thisFirst, out, at)
			}
			out.WriteObjectEnd()
		}
		needComma = true
		return true
	})
	for ; braceLevel > 0; braceLevel-- {
		out.WriteObjectEnd()
	}
	out.Flush()

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
	result := &LegacyHandler{
		destination: h.destination,
		level:       h.level,
		who:         h.who,
		attrs:       cloneBuilder(h.attrs),
		needComma:   h.needComma,
		braceLevel:  h.braceLevel,
	}
	out := jsoniter.NewStream(jsoniter.ConfigDefault, result.attrs, 50)
	defer out.Flush()

	outputEmpty := result.attrs.Len() == 0
	for _, attr := range attrs {
		if attr.Key == "who" {
			result.who = optional.New(attr.Value.String())
			continue
		}
		if attr.Value.Kind() == slog.KindGroup && len(attr.Value.Group()) == 0 {
			// JSONHandler omits empty group attributes, so we will, too.
			continue
		}
		if outputEmpty {
			out.WriteRaw(" ")
			out.WriteObjectStart()
			result.braceLevel++
			result.needComma = false
			outputEmpty = false
		}
		if result.needComma {
			out.WriteMore()
			out.WriteRaw(" ")
		}
		switch attr.Value.Kind() {
		case slog.KindString:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteString(attr.Value.String())
		case slog.KindInt64:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteInt64(attr.Value.Int64())
		case slog.KindUint64:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteUint64(attr.Value.Uint64())
		case slog.KindFloat64:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteFloat64(attr.Value.Float64())
		case slog.KindBool:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteBool(attr.Value.Bool())
		case slog.KindDuration:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteString(attr.Value.Duration().String())
		case slog.KindTime:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteString(attr.Value.Time().String())
		case slog.KindAny:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteVal(attr.Value.Any())
		case slog.KindGroup:
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteObjectStart()
			thisFirst := true
			for _, at := range attr.Value.Group() {
				attrToJson(&thisFirst, out, at)
			}
			out.WriteObjectEnd()
		}
		result.needComma = true
	}
	return result
}

func (h *LegacyHandler) WithGroup(name string) slog.Handler {
	result := &LegacyHandler{
		destination: h.destination,
		level:       h.level,
		who:         h.who,
		attrs:       cloneBuilder(h.attrs),
		needComma:   false,
		braceLevel:  h.braceLevel,
	}
	out := jsoniter.NewStream(jsoniter.ConfigDefault, result.attrs, 50)
	defer out.Flush()
	if result.attrs.Len() == 0 {
		out.WriteRaw(" ")
		out.WriteObjectStart()
		result.braceLevel++
	} else if h.needComma {
		out.WriteMore()
		out.WriteRaw(" ")
	}
	out.WriteObjectField(name)
	out.WriteRaw(" ")
	out.WriteObjectStart()
	result.braceLevel++
	return result
}
