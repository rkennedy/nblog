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

// baseHandler is a [slog.Handler] that writes log messages in the format of NetBackup legacy logs.
type baseHandler struct {
	destination       io.Writer
	level             slog.Leveler
	replaceAttr       ReplaceAttrFunc
	timestampFormat   string
	useFullCallerName bool
	numericSeverity   bool
}

var (
	_ slog.Handler  = &baseHandler{}
	_ legacyHandler = &baseHandler{}
)

// Option is a function that can be passed to [New] to configure a new [Handler].
type Option func(slog.Handler)

func base(h slog.Handler) *baseHandler {
	base, ok := h.(*baseHandler)
	if !ok {
		panic("option applied to wrong type")
	}
	return base
}

// Level configures a [Handler] to use the given logging level.
func Level(level slog.Leveler) Option {
	return func(h slog.Handler) {
		base(h).level = level
	}
}

// ReplaceAttr appends repl to the list of attribute-replacement functions that get applied to each attribute prior to
// being rendered into a log message.
func ReplaceAttr(repl ReplaceAttrFunc) Option {
	return func(h slog.Handler) {
		base(h).replaceAttr = ChainReplace(base(h).replaceAttr, repl)
	}
}

// TimestampFormat specifies the format used for formatting timestamps (à la [time.Time.Format]) in the logger output.
// If left unset, the default used will be [FullDateFormat]. The classic NetBackup format is [TimeOnlyFormat]; use that
// if log rotation would make the repeated inclusion of the date redundant.
func TimestampFormat(f string) Option {
	return func(h slog.Handler) {
		base(h).timestampFormat = f
	}
}

// UseFullCallerName indicates whether to include or omit the package-name portion of the caller in log messages. The
// default is to omit the package, so only the function name will appear.
func UseFullCallerName(use bool) Option {
	return func(h slog.Handler) {
		base(h).useFullCallerName = use
	}
}

// NumericSeverity configures the handler to record the log level as a number instead of a text label. Numbers used
// correspond to NetBackup severity levels, not [slog] levels:
//
//   - LevelDebug: 2
//   - LevelInfo: 4
//   - LevelWarn: 8
//   - LevelError: 16
func NumericSeverity(numeric bool) Option {
	return func(h slog.Handler) {
		base(h).numericSeverity = numeric
	}
}

// nestedCallback is a callback function for handlers. The leaf handler will provide a function that writes the
// attributes it received in the record so that the parent handler can call it when it's ready to render that portion of
// the log message. Each parent handler will provide another callback that writes its portion and then calls the child
// callback.
type nestedCallback func(*baseHandler, *jsonStream) uint

// legacyHandler is the interface for all the handler implementations in this file. They're all [slog.Handler] types,
// but they also include methods for returning the list of groups at their current nesting level, and for writing their
// portion of a log message.
type legacyHandler interface {
	slog.Handler

	groups() []string

	// writeWithContinuation will write the handler's portion of the log message. This method gets called recursively by
	// the child handlers along a chain of handlers. When the recursion reaches the base case, it uses writeNested to
	// render the "inner" portion of the log message represented by the child handlers. When the callback finally
	// returns to the recursion's base case, then it writes any necessary closing braces before finally copying the
	// rendered log message to the output stream.
	writeWithContinuation(out *jsonStream, record slog.Record, writeNested nestedCallback) error
}

// groupHandler is a child handler to represent the result of calling WithGroup.
type groupHandler struct {
	previousHandler legacyHandler
	group           string
}

var (
	_ slog.Handler  = &groupHandler{}
	_ legacyHandler = &groupHandler{}
)

// attrHandler is a child handler to represent the result of calling WithAttrs.
type attrHandler struct {
	previousHandler legacyHandler
	attributes      []slog.Attr
}

var (
	_ slog.Handler  = &attrHandler{}
	_ legacyHandler = &attrHandler{}
)

// New creates a new [slog.Handler]. It receives a destination [io.Writer] and options to configure the handler.
//
// When formatting a message, the handler calls any [ReplaceAttrFunc] callbacks on each attribute associated with the
// message. It will synthesize attributes representing the timestamp, process ID, level, and message, giving the program
// an opportunity to modify, replace, or remove any of them, just as for any other attributes. Such synthetic attributes
// are identified with the labels [slog.TimeKey], [PidKey], [slog.LevelKey], and [slog.MessageKey], respectively, each
// with an empty group array.
//
// If the replacement callback for the [slog.TimeKey] attribute returns a [time.Time] value, then it will be formatted
// with the configured [TimestampFormat] option.
func New(w io.Writer, opts ...Option) slog.Handler {
	handler := &baseHandler{
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
func (h *baseHandler) Enabled(_ context.Context, alev slog.Level) bool {
	return alev >= h.level.Level()
}

// Enabled implements [slog.Handler.Enabled].
func (h *groupHandler) Enabled(ctx context.Context, alev slog.Level) bool {
	return h.previousHandler.Enabled(ctx, alev)
}

// Enabled implements [slog.Handler.Enabled].
func (h *attrHandler) Enabled(ctx context.Context, alev slog.Level) bool {
	return h.previousHandler.Enabled(ctx, alev)
}

func commonWithAttrs(h legacyHandler, attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return &attrHandler{
		previousHandler: h,
		attributes:      attrs,
	}
}

// WithAttrs implements [slog.Handler.WithAttrs].
func (h *baseHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return commonWithAttrs(h, attrs)
}

// WithAttrs implements [slog.Handler.WithAttrs].
func (h *groupHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return commonWithAttrs(h, attrs)
}

// WithAttrs implements [slog.Handler.WithAttrs].
func (h *attrHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return commonWithAttrs(h, attrs)
}

func commonWithGroup(h legacyHandler, name string) slog.Handler {
	if name == "" {
		return h
	}
	return &groupHandler{
		previousHandler: h,
		group:           name,
	}
}

// WithGroup implements [slog.Handler.WithGroup].
func (h *baseHandler) WithGroup(name string) slog.Handler {
	return commonWithGroup(h, name)
}

// WithGroup implements [slog.Handler.WithGroup].
func (h *groupHandler) WithGroup(name string) slog.Handler {
	return commonWithGroup(h, name)
}

// WithGroup implements [slog.Handler.WithGroup].
func (h *attrHandler) WithGroup(name string) slog.Handler {
	return commonWithGroup(h, name)
}

func writeTimestamp(out *jsonStream, h *baseHandler, rec slog.Record) {
	if rec.Time.IsZero() {
		return
	}
	timeAttr := h.replaceAttrs([]string{}, slog.Time(slog.TimeKey, rec.Time))
	if timeAttr.Equal(slog.Attr{}) {
		return
	}
	var timestamp string
	if timeAttr.Value.Kind() == slog.KindTime {
		timestamp = timeAttr.Value.Time().Format(h.timestampFormat)
	} else {
		timestamp = timeAttr.Value.String()
	}
	out.WriteRaw(timestamp + " ")
}

func writePid(out *jsonStream, h *baseHandler, _ slog.Record) {
	pidAttr := h.replaceAttrs([]string{}, slog.Int(PidKey, os.Getpid()))
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

func writeLevel(out *jsonStream, h *baseHandler, rec slog.Record) {
	levelAttr := h.replaceAttrs([]string{}, slog.Any(slog.LevelKey, rec.Level))
	if levelAttr.Equal(slog.Attr{}) {
		return
	}
	if h.numericSeverity {
		leveler, ok := levelAttr.Value.Any().(slog.Leveler)
		if ok {
			newLevel := scaleLevel(leveler)
			levelAttr.Value = slog.Float64Value(newLevel)
		}
	}
	out.WriteRaw("<" + levelAttr.Value.String() + "> ")
}

func writeCaller(out *jsonStream, h *baseHandler, rec slog.Record) {
	if rec.PC == 0 {
		return
	}

	frames := runtime.CallersFrames([]uintptr{rec.PC})
	frame, _ := frames.Next()
	who := frame.Function
	if !h.useFullCallerName {
		lastDot := strings.LastIndex(who, ".")
		if lastDot >= 0 {
			who = who[lastDot+1:]
		}
	}
	out.WriteRaw(who + ": ")
}

func writeMessage(out *jsonStream, h *baseHandler, rec slog.Record) {
	msgAttr := h.replaceAttrs([]string{}, slog.String(slog.MessageKey, rec.Message))
	if msgAttr.Equal(slog.Attr{}) {
		return
	}
	out.WriteRaw(msgAttr.Value.String())
}

func (h *baseHandler) writeNextAttribute(a slog.Attr, out *jsonStream, groups []string) bool {
	a = h.replaceAttrs(groups, a)
	if !a.Equal(slog.Attr{}) {
		writeAttribute(out, h, groups, a)
	}
	return true
}

func (h *baseHandler) replaceAttrs(groups []string, attr slog.Attr) slog.Attr {
	if h.replaceAttr != nil {
		return h.replaceAttr(groups, attr)
	}
	return attr
}

func (*baseHandler) groups() []string {
	return nil
}

func (h *groupHandler) groups() []string {
	return append(h.previousHandler.groups(), h.group)
}

func (h *attrHandler) groups() []string {
	return h.previousHandler.groups()
}

// writingStepFunc is a function that will write one section of a log message to the jsonStream.
type writingStepFunc func(*jsonStream, *baseHandler, slog.Record)

// writeAttributes generates a [writingStepFunc] that will write the full list of attributes at the end of a log
// message. If there are no attributes to write, then the writeNested callback should be nil. Otherwise, the returned
// function will write a space, an opening brace, and as many closing braces as needed (as reported by the return value
// of the writeNested callback).
func writeAttributes(writeNested nestedCallback) writingStepFunc {
	return func(out *jsonStream, base *baseHandler, _ slog.Record) {
		if writeNested == nil {
			return
		}
		out.WriteRaw(" ")
		out.WriteObjectStart()
		for range 1 + writeNested(base, out) {
			out.WriteObjectEnd()
		}
	}
}

func writeEnd(out *jsonStream, _ *baseHandler, _ slog.Record) {
	out.WriteRaw("\n")
}

// writeWithContinuation renders the entire log message. Groups and attributes from child handlers are written by the
// writeNested callback function. This function writes all the other log information prior to writing the nested
// attributes.
func (h *baseHandler) writeWithContinuation(out *jsonStream, record slog.Record, writeNested nestedCallback) error {
	for _, writer := range []writingStepFunc{
		writeTimestamp,
		writePid,
		writeLevel,
		writeCaller,
		writeMessage,
		writeAttributes(writeNested),
		writeEnd,
	} {
		writer(out, h, record)
		if out.Error() != nil {
			return out.Error()
		}
	}

	if out.Error() != nil {
		return out.Error()
	}
	_, err := h.destination.Write(out.Buffer())
	return err
}

// writeWithContinuation generates a callback that will begin a JSON object for the handler's group when called by the
// parent log handler.
func (h *groupHandler) writeWithContinuation(out *jsonStream, record slog.Record, writeNested nestedCallback) error {
	var newWriteAttributes nestedCallback
	if writeNested != nil {
		// The child handler has attributes to write, so our group counts.
		newWriteAttributes = func(base *baseHandler, out *jsonStream) uint {
			out.WriteObjectField(h.group)
			out.WriteObjectStart()
			return 1 + writeNested(base, out)
		}
	}
	return h.previousHandler.writeWithContinuation(out, record, newWriteAttributes)
}

// writeWithContinuation generates a callback that will write the current handler's accumulated attributes when called
// by the parent log handler.
func (h *attrHandler) writeWithContinuation(out *jsonStream, record slog.Record, writeNested nestedCallback) error {
	newWriteAttributes := func(base *baseHandler, out *jsonStream) uint {
		for _, attr := range h.attributes {
			_ = base.writeNextAttribute(attr, out, h.groups())
		}
		if writeNested != nil {
			return writeNested(base, out)
		}
		return 0
	}
	return h.previousHandler.writeWithContinuation(out, record, newWriteAttributes)
}

func commonHandle(h legacyHandler, record slog.Record) error {
	out := newJSONStream()
	var writeAttributes nestedCallback
	if record.NumAttrs() != 0 {
		writeAttributes = func(base *baseHandler, out *jsonStream) uint {
			record.Attrs(func(a slog.Attr) bool {
				return base.writeNextAttribute(a, out, h.groups())
			})
			return 0
		}
	}
	return h.writeWithContinuation(out, record, writeAttributes)
}

// Handle implements [slog.Handler.Handle].
func (h *baseHandler) Handle(_ context.Context, record slog.Record) error {
	return commonHandle(h, record)
}

// Handle implements [slog.Handler.Handle].
func (h *groupHandler) Handle(_ context.Context, record slog.Record) error {
	return commonHandle(h, record)
}

// Handle implements [slog.Handler.Handle].
func (h *attrHandler) Handle(_ context.Context, record slog.Record) error {
	return commonHandle(h, record)
}
