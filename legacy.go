package nblog

import (
	"context"
	"io"
	"log/slog"
	"math"
	"os"
	"runtime"
	"strings"
	"time"
)

// These are formats for use with [TimestampFormat].
const (
	FullDateFormat = time.DateTime + ".000"
	TimeOnlyFormat = time.TimeOnly + ".000"
)

// PidKey is the reserved attribute key for the process ID when [Handler.Handle] does attribute-replacement.
const PidKey = "pid-47482072-7496-40a0-a048-ccfdba4e564e"

// Handler is a [slog.Handler] that writes log messages in the format of NetBackup legacy logs.
type Handler struct {
	destination io.Writer

	level             slog.Leveler
	replaceAttr       ReplaceAttrFunc
	timestampFormat   string
	useFullCallerName bool
	numericSeverity   bool

	previousHandler *Handler
	group           string
	attributes      []slog.Attr
}

var _ slog.Handler = &Handler{}

// Option is a function that can be passed to [New] to configure a new [Handler].
type Option func(*Handler)

// Level configures a [Handler] to use the given logging level.
func Level(level slog.Leveler) Option {
	return func(h *Handler) {
		h.level = level
	}
}

// ReplaceAttr appends repl to the list of attribute-replacement functions that get applied to each attribute prior to
// being rendered into a log message.
func ReplaceAttr(repl ReplaceAttrFunc) Option {
	return func(h *Handler) {
		h.replaceAttr = ChainReplace(h.replaceAttr, repl)
	}
}

// TimestampFormat specifies the format used for formatting timestamps (à la [time.Time.Format]) in the logger output.
// If left unset, the default used will be [FullDateFormat]. The classic NetBackup format is [TimeOnlyFormat]; use that
// if log rotation would make the repeated inclusion of the date redundant.
func TimestampFormat(f string) Option {
	return func(h *Handler) {
		h.timestampFormat = f
	}
}

// UseFullCallerName indicates whether to include or omit the package-name portion of the caller in log messages. The
// default is to omit the package, so only the function name will appear.
func UseFullCallerName(use bool) Option {
	return func(h *Handler) {
		h.useFullCallerName = use
	}
}

// NumericSeverity configures the handler to record the log level as a number instead of a text label. Numbers used
// correspond to NetBackup severity levels, not [slog] levels:
//
//  - LevelDebug: 2
//  - LevelInfo: 4
//  - LevelWarn: 8
//  - LevelError: 16
func NumericSeverity(numeric bool) Option {
	return func(h *Handler) {
		h.numericSeverity = numeric
	}
}

// New creates a new [Handler]. It receives a destination [io.Writer] and options to configure it.
func New(w io.Writer, opts ...Option) *Handler {
	handler := &Handler{
		destination: w,

		level:             slog.LevelInfo,
		replaceAttr:       nil,
		timestampFormat:   FullDateFormat,
		useFullCallerName: false,
		numericSeverity:   false,
	}
	for _, opt := range opts {
		opt(handler)
	}

	return handler
}

// Enabled implements [slog.Handler.Enabled].
func (h *Handler) Enabled(_ context.Context, alev slog.Level) bool {
	return alev >= getLevel(h).Level()
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
	timeAttr := getReplaceAttr(h)([]string{}, slog.Time(slog.TimeKey, rec.Time))
	if timeAttr.Equal(slog.Attr{}) {
		return
	}
	var timestamp string
	if timeAttr.Value.Kind() == slog.KindTime {
		timestamp = timeAttr.Value.Time().Format(getTimestampFormat(h))
	} else {
		timestamp = timeAttr.Value.String()
	}
	out.WriteRaw(timestamp + " ")
}

func writePid(out *jsonStream, h *Handler, _ slog.Record) {
	pidAttr := slog.Int(PidKey, os.Getpid())
	pidAttr = getReplaceAttr(h)([]string{}, pidAttr)
	if pidAttr.Equal(slog.Attr{}) {
		return
	}
	out.WriteRaw("[" + pidAttr.Value.String() + "] ")
}

func scaleLevel(leveler slog.Leveler) float64 {
	diff := float64(slog.LevelError - slog.LevelWarn)
	offset := 1 - float64(slog.LevelDebug)/diff
	return math.Pow(2, float64(leveler.Level())/diff+offset) //revive:disable-line:add-constant
}

func writeLevel(out *jsonStream, h *Handler, rec slog.Record) {
	levelAttr := getReplaceAttr(h)([]string{}, slog.Any(slog.LevelKey, rec.Level))
	if levelAttr.Equal(slog.Attr{}) {
		return
	}
	if getNumericSeverity(h) {
		leveler, ok := levelAttr.Value.Any().(slog.Leveler)
		if ok {
			newLevel := scaleLevel(leveler)
			levelAttr.Value = slog.Float64Value(newLevel)
		}
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
	if !getUseFullCallerName(h) {
		lastDot := strings.LastIndex(who, ".")
		if lastDot >= 0 {
			who = who[lastDot+1:]
		}
	}
	out.WriteRaw(who + ": ")
}

func writeMessage(out *jsonStream, h *Handler, rec slog.Record) {
	msgAttr := slog.String(slog.MessageKey, rec.Message)
	msgAttr = getReplaceAttr(h)([]string{}, msgAttr)
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

// writeParentGroupAndAttributes writes the groups and attributes stored in the handler h and any of its parent
// handlers. Returns the number of groups that are currently open.
//
//revive:disable-next-line:function-length There's no good way to make this any shorter.
func (h *Handler) writeParentGroupsAndAttributes( //revive:disable-line:flag-parameter
	out *jsonStream,
	hasChildren bool,
) uint {
	if h.previousHandler == nil {
		// If we got to the base case and there are no attributes, then exit without writing an empty set of braces.
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
	a = getReplaceAttr(h)(groups, a)
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
//
// When formatting a message, the handler calls any [ReplaceAttrFunc] callbacks on any attributes associated with the
// message. It will synthesize attributes representing the timestamp, process ID, level, and message, giving the program
// an opportunity to modify, replace, or remove any of them, just as for any other attributes. Such synthetic attributes
// are identified with the labels [slog.TimeKey], [PidKey], [slog.LevelKey], and [slog.MessageKey], respectively, each
// with an empty group array.
//
// If the replacement callback returns a [time.Time] value for the [slog.TimeKey] attribute, then it will be formatted
// with the configured [TimestampFormat] option.
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
