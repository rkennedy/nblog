package nblog

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Handler struct {
	destination   io.Writer
	nestedHandler slog.Handler
	buffer        *bytes.Buffer
	mutex         *sync.Mutex

	replaceAttr       ReplaceAttrFunc
	timestampFormat   string
	useFullCallerName bool
}

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
func New(w io.Writer, handlerOptions *HandlerOptions) *Handler {
	if handlerOptions == nil {
		handlerOptions = &HandlerOptions{}
	}

	buf := &bytes.Buffer{}
	handler := &Handler{
		destination: w,
		nestedHandler: slog.NewJSONHandler(buf, &slog.HandlerOptions{
			Level:       handlerOptions.Level,
			AddSource:   handlerOptions.AddSource,
			ReplaceAttr: ChainReplace(suppressDefaults, handlerOptions.ReplaceAttr),
		}),
		buffer: buf,
		mutex:  &sync.Mutex{},

		replaceAttr:       handlerOptions.ReplaceAttr,
		timestampFormat:   FullDateFormat,
		useFullCallerName: handlerOptions.UseFullCallerName,
	}
	if handlerOptions.TimestampFormat != "" {
		handler.timestampFormat = handlerOptions.TimestampFormat
	}
	if handlerOptions.NumericSeverity {
		handler.replaceAttr = ChainReplace(handler.replaceAttr, replaceNumericSeverity)
	}

	return handler
}

// Enabled implements [slog.Handler.Enabled].
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.nestedHandler.Enabled(ctx, level)
}

// WithAttrs implements [slog.Handler.WithAttrs].
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{
		nestedHandler:   h.nestedHandler.WithAttrs(attrs),
		destination:     h.destination,
		buffer:          h.buffer,
		mutex:           h.mutex,
		replaceAttr:     h.replaceAttr,
		timestampFormat: h.timestampFormat,
	}
}

// WithGroup implements [slog.Handler.WithGroup].
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{
		nestedHandler:   h.nestedHandler.WithGroup(name),
		destination:     h.destination,
		buffer:          h.buffer,
		mutex:           h.mutex,
		replaceAttr:     h.replaceAttr,
		timestampFormat: h.timestampFormat,
	}
}

func (h *Handler) computeAttrs(
	ctx context.Context,
	record slog.Record,
) ([]byte, error) {
	h.mutex.Lock()
	defer func() {
		h.buffer.Reset()
		h.mutex.Unlock()
	}()

	// We "compute" the attributes by making the nested JSONHandler print
	// the log message to our internal buffer, and then we parse that
	// output back into a map of attributes. We don't just use the
	// serialized output from the nested logger in our outbut because we
	// want to make the output a little nicer by adding spaces between keys
	// and values.
	if err := h.nestedHandler.Handle(ctx, record); err != nil {
		return nil, fmt.Errorf("error when calling inner handler's Handle: %w", err)
	}

	return h.buffer.Bytes(), nil
	/*
		var attrs map[string]any
		err := json.Unmarshal(h.buffer.Bytes(), &attrs)
		if err != nil {
			return nil, fmt.Errorf("error when unmarshaling inner handler's Handle result: %w", err)
		}
		return attrs, nil
	*/
}

func getCaller(rec slog.Record, useFullCallerName bool) string {
	frames := runtime.CallersFrames([]uintptr{rec.PC})
	frame, _ := frames.Next()
	who := frame.Function
	if !useFullCallerName {
		lastDot := strings.LastIndex(who, ".")
		if lastDot >= 0 {
			who = who[lastDot+1:]
		}
	}
	return who
}

func writeNonemptySeparated(out io.StringWriter, sep string, components ...string) error {
	needSep := false
	for _, comp := range components {
		if comp == "" {
			continue
		}
		if needSep {
			_, err := out.WriteString(sep)
			if err != nil {
				return err
			}
		}
		_, err := out.WriteString(comp)
		if err != nil {
			return err
		}
		needSep = true
	}
	return nil
}

// Handle implements [slog.Handler.Handle].
func (h *Handler) Handle(ctx context.Context, record slog.Record) error {
	var level string
	levelAttr := slog.Attr{
		Key:   slog.LevelKey,
		Value: slog.AnyValue(record.Level),
	}
	if h.replaceAttr != nil {
		levelAttr = h.replaceAttr([]string{}, levelAttr)
	}
	if !levelAttr.Equal(slog.Attr{}) {
		level = "<" + levelAttr.Value.String() + ">"
	}

	var timestamp string
	if !record.Time.IsZero() {
		timeAttr := slog.Time(slog.TimeKey, record.Time)
		if h.replaceAttr != nil {
			timeAttr = h.replaceAttr([]string{}, timeAttr)
		}
		if !timeAttr.Equal(slog.Attr{}) {
			if timeAttr.Value.Kind() == slog.KindTime {
				timestamp = timeAttr.Value.Time().Format(h.timestampFormat)
			} else {
				timestamp = timeAttr.Value.String()
			}
		}
	}

	var pid string
	pidAttr := slog.Attr{
		Key:   PidKey,
		Value: slog.IntValue(os.Getpid()),
	}
	if h.replaceAttr != nil {
		pidAttr = h.replaceAttr([]string{}, pidAttr)
	}
	if !pidAttr.Equal(slog.Attr{}) {
		pid = "[" + pidAttr.Value.String() + "]"
	}

	var caller string
	if record.PC != 0 {
		caller = getCaller(record, h.useFullCallerName) + ":"
	}

	var msg string
	msgAttr := slog.Attr{
		Key:   slog.MessageKey,
		Value: slog.StringValue(record.Message),
	}
	if h.replaceAttr != nil {
		msgAttr = h.replaceAttr([]string{}, msgAttr)
	}
	if !msgAttr.Equal(slog.Attr{}) {
		msg = msgAttr.Value.String()
	}

	attrsAsBytes, err := h.computeAttrs(ctx, record)
	if err != nil {
		return err
	}

	/*
		var attrsAsBytes []byte
		if len(attrs) > 0 {
			// TODO (rkennedy) Marshal JSON with nicer format.
			attrsAsBytes, err = json.Marshal(attrs)
			if err != nil {
				return fmt.Errorf("error when marshaling attrs: %w", err)
			}
		}
	*/
	if attrsAsBytes[len(attrsAsBytes)-1] == '\n' {
		attrsAsBytes = attrsAsBytes[0 : len(attrsAsBytes)-1]
	}
	if string(attrsAsBytes) == "{}" {
		attrsAsBytes = nil
	}

	out := strings.Builder{}
	err = writeNonemptySeparated(&out, " ",
		timestamp,
		pid,
		level,
		caller,
		msg,
		// TODO (rkennedy) This is a hack because JSON formatting is broken.
		strings.ReplaceAll(strings.ReplaceAll(string(attrsAsBytes), "\":", "\": "), ",\"", ", \""),
	)
	if err != nil {
		return err
	}

	_, err = io.WriteString(h.destination, out.String()+"\n")
	return err
}

func suppressDefaults(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey ||
		a.Key == slog.LevelKey ||
		a.Key == slog.MessageKey {
		return slog.Attr{}
	}
	return a
}
