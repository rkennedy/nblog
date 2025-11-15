package nblog

import (
	"context"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	jsoniter "github.com/json-iterator/go"
)

// Formats for use with [HandlerOptions.TimestampFormat].
const (
	FullDateFormat = time.DateTime + ".000"
	TimeOnlyFormat = time.TimeOnly + ".000"
)

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
	PidKey = "pid-47482072-7496-40a0-a048-ccfdba4e564e" // process ID
)

type Handler struct {
	destination io.Writer

	level             slog.Leveler
	replaceAttr       ReplaceAttrFunc
	timestampFormat   string
	useFullCallerName bool

	previousHandler *Handler
	group           string
	attributes      []slog.Attr
}

// HandlerOptions describes the options available for nblog logger options. The
// AddSource, Level, and ReplaceAttr fields are as for [slog.HandlerOptions].
type HandlerOptions struct {
	AddSource   bool
	Level       slog.Leveler
	ReplaceAttr ReplaceAttrFunc

	// TimestampFormat specifies the [time] format used for formatting
	// timestamps (à la [time.Time.Format]) in the logger output. If left
	// unset, the default used will be [FullDateFormat]. The classic
	// NetBackup format is [TimeOnlyFormat]; use that if log rotation would
	// make the repeated inclusion of the date redundant.
	TimestampFormat string

	// UseFullCallerName determines whether to include or omit the
	// package-name portion of the caller in log messages. The default is
	// to omit the package, so only the function name will appear.
	UseFullCallerName bool

	// NumericSeverity configures the handler to record
	// the log level as a number instead of a text label.
	// Numbers used correspond to NetBackup severity
	// levels, not [slog] levels:
	//
	// - LevelDebug: 2
	// - LevelInfo: 4
	// - LevelWarn: 8
	// - LevelError: 16
	NumericSeverity bool
}

// New creates a new [Handler]. It receives a destination [io.Writer] and
// options to configure it.
func New(w io.Writer, opts *HandlerOptions) *Handler {
	if opts == nil {
		opts = &HandlerOptions{}
	}

	handler := &Handler{
		destination: w,

		level:             slog.LevelInfo,
		replaceAttr:       ChainReplace(opts.ReplaceAttr, nil),
		timestampFormat:   FullDateFormat,
		useFullCallerName: opts.UseFullCallerName,
	}
	if opts.Level != nil {
		handler.level = opts.Level
	}
	if opts.TimestampFormat != "" {
		handler.timestampFormat = opts.TimestampFormat
	}
	if opts.NumericSeverity {
		handler.replaceAttr = ChainReplace(handler.replaceAttr, replaceNumericSeverity)
	}

	return handler
}

// Enabled implements [slog.Handler.Enabled].
func (h *Handler) Enabled(_ context.Context, alev slog.Level) bool {
	return alev >= level(h).Level()
}

// WithAttrs implements [slog.Handler.WithAttrs].
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return &Handler{
		previousHandler: h,
		attributes:      attrs,
	}
}

// WithGroup implements [slog.Handler.WithGroup].
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &Handler{
		previousHandler: h,
		group:           name,
	}
}

func writeTimestamp(out io.StringWriter, h *Handler, rec slog.Record) error {
	if rec.Time.IsZero() {
		return nil
	}
	timeAttr := slog.Time(slog.TimeKey, rec.Time)
	timeAttr = replaceAttr(h)([]string{}, timeAttr)
	if timeAttr.Equal(slog.Attr{}) {
		return nil
	}
	var timestamp string
	if timeAttr.Value.Kind() == slog.KindTime {
		timestamp = timeAttr.Value.Time().Format(timestampFormat(h))
	} else {
		timestamp = timeAttr.Value.String()
	}
	_, err := out.WriteString(timestamp + " ")
	return err
}

func writePid(out io.StringWriter, h *Handler) error {
	pidAttr := slog.Attr{
		Key:   PidKey,
		Value: slog.IntValue(os.Getpid()),
	}
	pidAttr = replaceAttr(h)([]string{}, pidAttr)
	if pidAttr.Equal(slog.Attr{}) {
		return nil
	}
	_, err := out.WriteString("[" + pidAttr.Value.String() + "] ")
	return err
}

func writeLevel(out io.StringWriter, h *Handler, rec slog.Record) error {
	levelAttr := slog.Attr{
		Key:   slog.LevelKey,
		Value: slog.AnyValue(rec.Level),
	}
	levelAttr = replaceAttr(h)([]string{}, levelAttr)
	if levelAttr.Equal(slog.Attr{}) {
		return nil
	}
	_, err := out.WriteString("<" + levelAttr.Value.String() + "> ")
	return err
}

func writeCaller(out io.StringWriter, h *Handler, rec slog.Record) error {
	if rec.PC == 0 {
		return nil
	}

	frames := runtime.CallersFrames([]uintptr{rec.PC})
	frame, _ := frames.Next()
	who := frame.Function
	if !useFullCallerName(h) {
		lastDot := strings.LastIndex(who, ".")
		if lastDot >= 0 {
			who = who[lastDot+1:]
		}
	}
	_, err := out.WriteString(who + ": ")
	return err
}

func writeMessage(out io.StringWriter, h *Handler, rec slog.Record) error {
	msgAttr := slog.Attr{
		Key:   slog.MessageKey,
		Value: slog.StringValue(rec.Message),
	}
	msgAttr = replaceAttr(h)([]string{}, msgAttr)
	if msgAttr.Equal(slog.Attr{}) {
		return nil
	}
	_, err := out.WriteString(msgAttr.Value.String())
	return err
}

func writeAttribute(out *jsoniter.Stream, h *Handler, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
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
		if attr.Key != "" {
			out.WriteObjectField(attr.Key)
			out.WriteRaw(" ")
			out.WriteObjectStart()
			groups = append(groups, attr.Key)
		}
		needComma := false
		for _, at := range attr.Value.Group() {
			at = replaceAttr(h)(groups, at)
			if at.Equal(slog.Attr{}) {
				continue
			}
			if needComma {
				out.WriteMore()
				out.WriteRaw(" ")
			}
			writeAttribute(out, h, groups, at)
			needComma = true
		}
		if attr.Key != "" {
			out.WriteObjectEnd()
		}
	}
}

func writeParentGroupsAndAttributes(out *jsoniter.Stream, h *Handler, hasChildren bool) (
	openGroups uint, needComma bool,
) {
	if h.previousHandler == nil {
		// If we got to the base case and there are no attributes, then
		// exit without writing an empty set of braces.
		if !hasChildren {
			return 0, false
		}
		out.WriteRaw(" ")
		out.WriteObjectStart()
		return 1, false
	}

	openGroups, needComma = writeParentGroupsAndAttributes(out, h.previousHandler, hasChildren || len(h.attributes) > 0)

	if h.group != "" {
		// Open a group
		if needComma {
			out.WriteMore()
			out.WriteRaw(" ")
		}
		out.WriteObjectField(h.group)
		out.WriteObjectStart()
		return openGroups + 1, false
	}
	for _, attr := range h.attributes {
		attr = replaceAttr(h)(h.groups(), attr)
		if attr.Equal(slog.Attr{}) {
			continue
		}
		if needComma {
			out.WriteMore()
			out.WriteRaw(" ")
		}
		writeAttribute(out, h, h.groups(), attr)
		needComma = true
	}
	return openGroups, needComma
}

func writeAttributes(out *jsoniter.Stream, h *Handler, rec slog.Record) {
	if rec.NumAttrs() == 0 {
		for h.previousHandler != nil && h.group != "" {
			// We're in a group, but we have no attributes. Omit this group.
			h = h.previousHandler
		}
	}
	// Go to the head of the list and start writing groups and attributes.
	openGroups, needComma := writeParentGroupsAndAttributes(out, h, rec.NumAttrs() > 0)

	// Then write the attributes in rec.Attrs.
	rec.Attrs(func(a slog.Attr) bool {
		a = replaceAttr(h)(h.groups(), a)
		if a.Equal(slog.Attr{}) {
			return true
		}
		if needComma {
			out.WriteMore()
			out.WriteRaw(" ")
		}
		writeAttribute(out, h, h.groups(), a)
		needComma = true
		return true
	})
	// Finally, close any open groups.
	for range openGroups {
		out.WriteObjectEnd()
	}
}

// Handle implements [slog.Handler.Handle].
func (h *Handler) Handle(_ context.Context, record slog.Record) error {
	out := strings.Builder{}

	err := writeTimestamp(&out, h, record)
	if err != nil {
		return err
	}

	err = writePid(&out, h)
	if err != nil {
		return err
	}

	err = writeLevel(&out, h, record)
	if err != nil {
		return err
	}

	err = writeCaller(&out, h, record)
	if err != nil {
		return err
	}

	err = writeMessage(&out, h, record)
	if err != nil {
		return err
	}

	jout := jsoniter.NewStream(jsoniter.Config{}.Froze(), &out, 50)
	writeAttributes(jout, h, record)
	if jout.Error != nil {
		return jout.Error
	}
	jout.Flush()

	_, err = io.WriteString(destination(h), out.String()+"\n")
	return err
}
