// Package nblog provides a handler for [golang.org/x/exp/slog] that formats
// logs in the style of Veritas NetBackup log messages.
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

// ReplaceAttrFunc is the type of callback used with [ReplaceAttrs] to allow
// editing, replacing, or removing of attributes nefore a log record is
// recorded. The function will be called for each non-group attribute, along
// with a list of the currently nested groups. The function can return the
// original attribute to log it as-is, return a different attribute to use it
// instead, or return an attribute with an empty Key value to omit the current
// attribute entirely.
type ReplaceAttrFunc func(groups []string, attr slog.Attr) slog.Attr

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

	attrs            *strings.Builder
	needComma        bool
	braceLevel       uint
	groups           []string
	replaceAttrFuncs []ReplaceAttrFunc
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

// ReplaceAttrs returns a [LegacyOption] callback that will configure a handler
// to include the given [ReplaceAttrFunc] callback function while processing log
// attributes prior to being recorded. Callbacks are called in the order
// they're added to the handler.
func ReplaceAttrs(replaceAttr ReplaceAttrFunc) LegacyOption {
	return func(handler *LegacyHandler) {
		handler.replaceAttrFuncs = append(handler.replaceAttrFuncs, replaceAttr)
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

func (h *LegacyHandler) attrToJson(needComma *bool, out *jsoniter.Stream, attr slog.Attr, beforeWrite func(), groups []string) {
	if attr.Key == "who" {
		return
	}
	if attr.Value.Kind() == slog.KindGroup {
		if len(attr.Value.Group()) == 0 {
			// JSONHandler omits empty group attributes,
			// so we will, too.
			return
		}
	} else {
		attr = h.replaceAttr(groups, attr)
		if attr.Key == "" {
			return
		}
	}

	if beforeWrite != nil {
		beforeWrite()
	}

	if *needComma {
		out.WriteMore()
		out.WriteRaw(" ")
	}
	out.WriteObjectField(attr.Key)
	out.WriteRaw(" ")

	switch attr.Value.Kind() {
	case slog.KindString:
		out.WriteString(attr.Value.String())
	case slog.KindInt64:
		out.WriteInt64(attr.Value.Int64())
	case slog.KindUint64:
		out.WriteUint64(attr.Value.Uint64())
	case slog.KindFloat64:
		out.WriteFloat64(attr.Value.Float64())
	case slog.KindBool:
		out.WriteBool(attr.Value.Bool())
	case slog.KindDuration:
		out.WriteString(attr.Value.Duration().String())
	case slog.KindTime:
		out.WriteString(attr.Value.Time().String())
	case slog.KindAny:
		out.WriteVal(attr.Value.Any())
	case slog.KindGroup:
		out.WriteObjectStart()
		thisNeedComma := false
		thisGroups := append(groups, attr.Key)
		for _, at := range attr.Value.Group() {
			h.attrToJson(&thisNeedComma, out, at, nil, thisGroups)
		}
		out.WriteObjectEnd()
	}
	*needComma = true
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

const (
	// TimeKey is a sentinel value denoting the message timestamp
	// for attribute replacement. The [ReplaceAttrFunc] callback
	// will receive a Time attribute having this key. If the
	// callback returns a Time attribute, its value will be
	// formatted as from the [TimestampFormat] option. Returning
	// an attribute with an empty key will omit the value as
	// usual. Any other attribute result will have its String
	// value used.
	TimeKey = "time-75972059-5741-41f7-9248-e8594177835c"
	// PidKey is a sentinel value denoting the process ID of the
	// message for attribute replacement. The [ReplaceAttrFunc]
	// callback will receive an Int attribute having this key.
	// Returning an attribute with an empty key will omit the
	// value as usual. Any other attribute result will have its
	// String value used.
	PidKey = "pid-47482072-7496-40a0-a048-ccfdba4e564e"
	// LevelKey is a sentinel value denoting the severity level
	// of the message for attribute replacement. The
	// [ReplaceAttrFunc] callback will receive an Any attribute
	// having this key. Returning an attribute with an empty key
	// will omit the value as usual. Any other attribute result
	// will have its String value used. The returned attribute
	// will have no effect on level-based message filtering;
	// returning a lower severity will not hide a message.
	LevelKey = "level-933f69a5-69b4-4f8a-a6a6-14810b97fdad"
	// MessageKey is a sentinel value denoting the message for
	// attribute replacement. The [ReplaceAttrFunc] callback will
	// receive a String attribute having this key. Returning an
	// attribute with an empty key will omit the value as usual.
	// Any other attribute result will have its String value
	// used.
	MessageKey = "message-5ae1bf30-54b2-4d50-8af7-7076b3a39e20"
)

func appendNonempty(s []string, value string) []string {
	if value != "" {
		s = append(s, value)
	}
	return s
}

func (h *LegacyHandler) replaceAttr(groups []string, attr slog.Attr) slog.Attr {
	for _, fn := range h.replaceAttrFuncs {
		attr = fn(groups, attr)
		if attr.Key == "" {
			break
		}
	}
	return attr
}

func (h *LegacyHandler) Handle(ctx context.Context, rec slog.Record) error {
	var parts []string

	if !rec.Time.IsZero() {
		format := h.timestampFormat.OrElse(FullDateFormat)
		timeAttr := h.replaceAttr(nil, slog.Time(TimeKey, rec.Time))
		if timeAttr.Key != "" {
			if timeAttr.Value.Kind() == slog.KindTime {
				parts = append(parts, timeAttr.Value.Time().Format(format))
			} else {
				parts = append(parts, timeAttr.Value.String())
			}
		}
	}

	pidAttr := h.replaceAttr(nil, slog.Int(PidKey, os.Getpid()))
	if pidAttr.Key != "" {
		parts = append(parts, fmt.Sprintf("[%s]", pidAttr.Value.String()))
	}

	levelAttr := h.replaceAttr(nil, slog.Any(LevelKey, rec.Level))
	if levelAttr.Key != "" {
		parts = append(parts, fmt.Sprintf("<%s>", levelAttr.Value.String()))
	}

	who := h.who
	rec.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "who" {
			who = optional.New(attr.Value.String())
		}
		return true
	})
	parts = append(parts, who.OrElseGet(func() string {
		return getCaller(rec, !h.useFullCallerName)
	})+":")

	messageAttr := h.replaceAttr(nil, slog.String(MessageKey, rec.Message))
	if messageAttr.Key != "" {
		parts = appendNonempty(parts, messageAttr.Value.String())
	}

	attributeBuffer := cloneBuilder(h.attrs)
	out := jsoniter.NewStream(jsoniter.ConfigDefault, attributeBuffer, 50)
	braceLevel := h.braceLevel
	needComma := h.needComma
	outputEmpty := h.attrs.Len() == 0

	beforeWrite := func() {
		if outputEmpty {
			outputEmpty = false
			out.WriteObjectStart()
			braceLevel++
			needComma = false
		}
	}

	rec.Attrs(func(attr slog.Attr) bool {
		h.attrToJson(&needComma, out, attr, beforeWrite, h.groups)
		return true
	})
	for ; braceLevel > 0; braceLevel-- {
		out.WriteObjectEnd()
	}
	out.Flush()

	if attrString := attributeBuffer.String(); attrString != "" {
		parts = append(parts, attrString)
	}

	fmt.Fprintln(h.destination, strings.Join(parts, " "))
	return nil
}

func (h *LegacyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	result := &LegacyHandler{
		destination:      h.destination,
		level:            h.level,
		who:              h.who,
		attrs:            cloneBuilder(h.attrs),
		needComma:        h.needComma,
		braceLevel:       h.braceLevel,
		groups:           h.groups,
		replaceAttrFuncs: h.replaceAttrFuncs,
	}
	out := jsoniter.NewStream(jsoniter.ConfigDefault, result.attrs, 50)
	defer out.Flush()

	outputEmpty := result.attrs.Len() == 0

	beforeWrite := func() {
		if outputEmpty {
			outputEmpty = false
			out.WriteObjectStart()
			result.braceLevel++
			result.needComma = false
		}
	}

	for _, attr := range attrs {
		if attr.Key == "who" {
			result.who = optional.New(attr.Value.String())
		}
		result.attrToJson(&result.needComma, out, attr, beforeWrite, result.groups)
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
