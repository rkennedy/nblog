package nblog

import (
	"context"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"
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

func writeTimestamp(out *jsonStream, h *Handler, rec slog.Record) {
	if rec.Time.IsZero() {
		return
	}
	timeAttr := replaceAttr(h)([]string{}, slog.Time(slog.TimeKey, rec.Time))
	if timeAttr.Equal(slog.Attr{}) {
		return
	}
	var timestamp string
	if timeAttr.Value.Kind() == slog.KindTime {
		timestamp = timeAttr.Value.Time().Format(timestampFormat(h))
	} else {
		timestamp = timeAttr.Value.String()
	}
	out.WriteRaw(timestamp + " ")
}

func writePid(out *jsonStream, h *Handler, _ slog.Record) {
	pidAttr := slog.Int(PidKey, os.Getpid())
	pidAttr = replaceAttr(h)([]string{}, pidAttr)
	if pidAttr.Equal(slog.Attr{}) {
		return
	}
	out.WriteRaw("[" + pidAttr.Value.String() + "] ")
}

func writeLevel(out *jsonStream, h *Handler, rec slog.Record) {
	levelAttr := slog.Any(slog.LevelKey, rec.Level)
	levelAttr = replaceAttr(h)([]string{}, levelAttr)
	if levelAttr.Equal(slog.Attr{}) {
		return
	}
	out.WriteRaw("<" + levelAttr.Value.String() + "> ")
}

func writeCaller(out *jsonStream, h *Handler, rec slog.Record) {
	if rec.PC == 0 {
		return
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
	out.WriteRaw(who + ": ")
}

func writeMessage(out *jsonStream, h *Handler, rec slog.Record) {
	msgAttr := slog.String(slog.MessageKey, rec.Message)
	msgAttr = replaceAttr(h)([]string{}, msgAttr)
	if msgAttr.Equal(slog.Attr{}) {
		return
	}
	out.WriteRaw(msgAttr.Value.String())
}

func writeAttribute(out *jsonStream, h *Handler, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	switch attr.Value.Kind() {
	case slog.KindGroup:
		writeGroup(out, h, groups, attr)
	default:
		write, ok := writeByKind[attr.Value.Kind()]
		if !ok {
			panic("No writer found for value")
		}
		write(out, attr)
	}
}

// writeParentGroupAndAttributes writes the groups and attributes stored in the
// handler h and any of its parent handlers. Returns the number of groups that
// are currently open.
func (h *Handler) writeParentGroupsAndAttributes(out *jsonStream, hasChildren bool) uint {
	if h.previousHandler == nil {
		// If we got to the base case and there are no attributes, then
		// exit without writing an empty set of braces.
		if !hasChildren {
			return 0
		}
		out.WriteRaw(" ")
		out.WriteObjectStart()
		return 1
	}

	openGroups := h.previousHandler.writeParentGroupsAndAttributes(out, hasChildren || len(h.attributes) > 0)

	if h.group != "" {
		// Open a group
		out.WriteObjectField(h.group)
		out.WriteObjectStart()
		return openGroups + 1
	}
	for _, attr := range h.attributes {
		_ = writeNextAttribute(attr, out, h, h.groups())
	}
	return openGroups
}

func writeNextAttribute(a slog.Attr, out *jsonStream, h *Handler, groups []string) bool {
	a = replaceAttr(h)(groups, a)
	if !a.Equal(slog.Attr{}) {
		writeAttribute(out, h, groups, a)
	}
	return true
}

func writeAttributes(out *jsonStream, h *Handler, rec slog.Record) {
	if rec.NumAttrs() == 0 {
		for h.previousHandler != nil && h.group != "" {
			// We're in a group, but we have no attributes. Omit this group.
			h = h.previousHandler
		}
	}
	// Go to the head of the list and start writing groups and attributes.
	openGroups := h.writeParentGroupsAndAttributes(out, rec.NumAttrs() > 0)

	// Then write the attributes in rec.Attrs.
	rec.Attrs(func(a slog.Attr) bool {
		return writeNextAttribute(a, out, h, h.groups())
	})
	// Finally, close any open groups.
	for range openGroups {
		out.WriteObjectEnd()
	}
}

// Handle implements [slog.Handler.Handle].
func (h *Handler) Handle(_ context.Context, record slog.Record) error {
	out := newJSONStream()

	for _, writer := range []func(*jsonStream, *Handler, slog.Record){
		writeTimestamp,
		writePid,
		writeLevel,
		writeCaller,
		writeMessage,
		writeAttributes,
	} {
		writer(out, h, record)
		if out.Error() != nil {
			return out.Error()
		}
	}
	out.WriteRaw("\n")

	_, err := destination(h).Write(out.Buffer())
	return err
}
