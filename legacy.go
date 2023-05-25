// Package nblog provides a handler for [slog] that formats logs in the style
// of Veritas NetBackup log messages.
package nblog

import (
	"context"
	"fmt"
	"io"
	"math"
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

// LegacyHandler is an implementation of [slog.Handler] that mimics the format
// used by legacy NetBackup logging. Attributes, if present, are appended to
// the line after the given message.
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
// [TimestampFormat] to generate callbacks to apply variable values. Applying
// LegacyOption values outside the context of NewHandler is not supported.
type LegacyOption func(handler *LegacyHandler)

// Level returns a [LegacyOption] that will configure a handler to filter out
// messages with a level lower than the given level.
func Level(level slog.Leveler) LegacyOption {
	return func(handler *LegacyHandler) {
		handler.level = level
	}
}

// Formats for use with [TimestampFormat].
const (
	FullDateFormat = time.DateTime + ".000"
	TimeOnlyFormat = time.TimeOnly + ".000"
)

// TimestampFormat returns a [LegacyOption] that will configure a handler to
// use the given time format (à la [time.Time.Format]) for log timestamps at
// the start of each record. If left unset, the default used will be
// [FullDateFormat]. The classic NetBackup format is [TimeOnlyFormat]; use that
// if log rotation would make the repeated inclusion of the date redundant.
func TimestampFormat(format string) LegacyOption {
	return func(handler *LegacyHandler) {
		handler.timestampFormat = optional.New(format)
	}
}

// UseFullCallerName returns a [LegacyOption] that will configure a handler to
// include or omit the package-name portion of the caller in log messages. The
// default is to omit the package, so only the function name will appear.
func UseFullCallerName(use bool) LegacyOption {
	return func(handler *LegacyHandler) {
		handler.useFullCallerName = use
	}
}

// NumericSeverity configures a handler to record the log level as a number
// instead of a text label. Numbers used correspond to NetBackup severity
// levels, not [slog] levels:
//
// - LevelDebug: 2
// - LevelInfo: 4
// - LevelWarn: 8
// - LevelError: 16
func NumericSeverity() LegacyOption {
	return ReplaceAttrs(replaceNumericSeverity)
}

// ReplaceAttrs returns a [LegacyOption] that configures a handler to include
// the given [ReplaceAttrFunc] callback function while processing log
// attributes prior to being recorded. During log-formatting, callbacks are
// called in the order they're added to the handler.
func ReplaceAttrs(replaceAttr ReplaceAttrFunc) LegacyOption {
	return func(handler *LegacyHandler) {
		handler.replaceAttrFuncs = append(handler.replaceAttrFuncs, replaceAttr)
	}
}

// NewHandler creates a new [LegacyHandler]. It receives a destination
// [io.Writer] and a list of [LegacyOption] values to configure it.
func NewHandler(dest io.Writer, options ...LegacyOption) *LegacyHandler {
	result := &LegacyHandler{
		destination:       dest,
		level:             slog.LevelInfo,
		useFullCallerName: false,
		attrs:             &strings.Builder{},
	}
	ReplaceAttrs(filterCaller)(result)
	for _, opt := range options {
		opt(result)
	}
	return result
}

func filterCaller(_ []string, attr slog.Attr) slog.Attr {
	if attr.Key == "who" {
		return slog.Attr{}
	}
	return attr
}

func replaceNumericSeverity(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) == 0 && attr.Key == LevelKey {
		leveler, ok := attr.Value.Any().(slog.Leveler)
		if ok {
			level := leveler.Level()
			diff := float64(slog.LevelError - slog.LevelWarn)
			offset := 1 - float64(slog.LevelDebug)/diff
			newLevel := math.Pow(2, float64(level)/diff+offset) //revive:disable-line:add-constant
			attr.Value = slog.Float64Value(newLevel)
		}
	}
	return attr
}

// Enabled implements [slog.Handler.Enabled].
func (h *LegacyHandler) Enabled(_ context.Context, level slog.Level) bool {
	return h.level.Level() <= level
}

const singleSpace = " "

func (h *LegacyHandler) attrToJSON(needComma *bool, out *jsoniter.Stream, attr slog.Attr,
	beforeWrite writePreparer, groups []string,
) {
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

	beforeWrite.PrepareToWrite(out, needComma)

	if *needComma {
		out.WriteMore()
		out.WriteRaw(singleSpace)
	}
	writeField(out, attr.Key)
	writeAttribute(h, attr, out, groups)

	*needComma = true
}

func writeAttribute(h *LegacyHandler, attr slog.Attr, out *jsoniter.Stream, groups []string) {
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
		needComma := false
		thisGroups := append(groups, attr.Key)
		for _, at := range attr.Value.Group() {
			h.attrToJSON(&needComma, out, at, &nullWritePreparer{}, thisGroups)
		}
		out.WriteObjectEnd()
	}
}

func cloneBuilder(b *strings.Builder) *strings.Builder {
	result := &strings.Builder{}
	_, _ = result.WriteString(b.String())
	return result
}

// When formatting a message, the handler calls any [ReplaceAttrFunc] callbacks
// on any attributes associated with the message. It will synthesize attributes
// representing the timestamp, process ID, level, and message, giving the
// program an opportunity to modify, replace, or remove any of them, just as
// for any other attributes. Such synthetic attributes are identified with
// these labels, which should be unique enough not to collide with any
// attribute keys in the program.
//
// If the replacement callback returns a [time.Time] value for the “time”
// attribute, then it will be formatted with the configured [TimestampFormat]
// option. Othe types for “time,” as well as other synthetic attributes, are
// recorded in the log with [slog.Value.String].
const (
	TimeKey    = "time-75972059-5741-41f7-9248-e8594177835c"  // message timestamp
	PidKey     = "pid-47482072-7496-40a0-a048-ccfdba4e564e"   // process ID
	LevelKey   = "level-933f69a5-69b4-4f8a-a6a6-14810b97fdad" // severity level
	MessageKey = "message-5ae1bf30-54b2-4d50-8af7-7076b3a39e20"
)

func concatNonempty(strs ...string) (result []string) {
	for _, value := range strs {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
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

const jsonStreamSize = 50 // The size is arbitrary.

func (h *LegacyHandler) getTime(rec slog.Record) string {
	if rec.Time.IsZero() {
		return ""
	}
	format := h.timestampFormat.OrElse(FullDateFormat)
	timeAttr := h.replaceAttr(nil, slog.Time(TimeKey, rec.Time))
	if timeAttr.Key == "" {
		return ""
	}
	if timeAttr.Value.Kind() == slog.KindTime {
		return timeAttr.Value.Time().Format(format)
	}
	return timeAttr.Value.String()
}

func (h *LegacyHandler) getCaller(rec slog.Record) string {
	who := h.who
	rec.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "who" {
			who = optional.New(attr.Value.String())
			return false
		}
		return true
	})
	return who.OrElseGet(func() string {
		frames := runtime.CallersFrames([]uintptr{rec.PC})
		frame, _ := frames.Next()
		who := frame.Function
		if !h.useFullCallerName {
			lastDot := strings.LastIndex(who, ".")
			if lastDot >= 0 {
				who = who[lastDot+1:]
			}
		}
		return who
	})
}

// Handle implements [slog.Handler.Handle].
func (h *LegacyHandler) Handle(_ context.Context, rec slog.Record) error {
	var pid, level, message string

	if pidAttr := h.replaceAttr(nil, slog.Int(PidKey, os.Getpid())); pidAttr.Key != "" {
		pid = fmt.Sprintf("[%s]", pidAttr.Value.String())
	}

	if levelAttr := h.replaceAttr(nil, slog.Any(LevelKey, rec.Level)); levelAttr.Key != "" {
		level = fmt.Sprintf("<%s>", levelAttr.Value.String())
	}

	if messageAttr := h.replaceAttr(nil, slog.String(MessageKey, rec.Message)); messageAttr.Key != "" {
		message = messageAttr.Value.String()
	}

	attributeBuffer := cloneBuilder(h.attrs)
	h.formatUserAttributes(attributeBuffer, rec)

	parts := concatNonempty(
		h.getTime(rec),
		pid,
		level,
		h.getCaller(rec)+":",
		message,
		attributeBuffer.String(),
	)

	_, err := fmt.Fprintln(h.destination, strings.Join(parts, singleSpace))
	return err
}

// formatUserAttributes formats any attributes provided in the immediate Log function.
func (h *LegacyHandler) formatUserAttributes(buffer io.Writer, rec slog.Record) {
	out := jsoniter.NewStream(jsoniter.ConfigDefault, buffer, jsonStreamSize)
	defer out.Flush()
	braceLevel := h.braceLevel
	needComma := h.needComma

	beforeWrite := mainWritePreparer{
		outputEmpty: h.attrs.Len() == 0,
		braceLevel:  &braceLevel,
	}

	rec.Attrs(func(attr slog.Attr) bool {
		h.attrToJSON(&needComma, out, attr, &beforeWrite, h.groups)
		return true
	})
	for ; braceLevel > 0; braceLevel-- {
		out.WriteObjectEnd()
	}
}

type writePreparer interface {
	PrepareToWrite(out *jsoniter.Stream, needComma *bool)
}

type mainWritePreparer struct {
	outputEmpty bool
	braceLevel  *uint
}

func (prep *mainWritePreparer) PrepareToWrite(out *jsoniter.Stream, needComma *bool) {
	if prep.outputEmpty {
		prep.outputEmpty = false
		out.WriteObjectStart()
		*prep.braceLevel++
		*needComma = false
	}
}

type nullWritePreparer struct {
}

func (*nullWritePreparer) PrepareToWrite(*jsoniter.Stream, *bool) {
}

// WithAttrs implements [slog.Handler.WithAttrs].
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
	out := jsoniter.NewStream(jsoniter.ConfigDefault, result.attrs, jsonStreamSize)
	defer out.Flush()

	beforeWrite := mainWritePreparer{
		outputEmpty: result.attrs.Len() == 0,
		braceLevel:  &result.braceLevel,
	}

	for _, attr := range attrs {
		if attr.Key == "who" {
			result.who = optional.New(attr.Value.String())
		} else {
			result.attrToJSON(&result.needComma, out, attr, &beforeWrite, result.groups)
		}
	}
	return result
}

func writeField(out *jsoniter.Stream, name string) {
	out.WriteObjectField(name)
	out.WriteRaw(singleSpace)
}

// WithGroup implements [slog.Handler.WithGroup].
func (h *LegacyHandler) WithGroup(name string) slog.Handler {
	result := &LegacyHandler{
		destination: h.destination,
		level:       h.level,
		who:         h.who,
		attrs:       cloneBuilder(h.attrs),
		needComma:   false,
		braceLevel:  h.braceLevel,
	}
	out := jsoniter.NewStream(jsoniter.ConfigDefault, result.attrs, jsonStreamSize)
	defer out.Flush()
	if result.attrs.Len() == 0 {
		out.WriteObjectStart()
		result.braceLevel++
	} else if h.needComma {
		out.WriteMore()
		out.WriteRaw(singleSpace)
	}
	writeField(out, name)
	out.WriteObjectStart()
	result.braceLevel++
	return result
}
