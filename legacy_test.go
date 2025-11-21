package nblog_test

//revive:disable:add-constant,function-length
import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"testing/slogtest"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	. "github.com/onsi/gomega"
	"github.com/rkennedy/nblog"
)

const (
	FullTimestampRegex = `\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}`
	TimeOnlyRegex      = `\d{2}:\d{2}:\d{2}\.\d{3}`
	PidRegex           = `\d+`
	ThisPackage        = "github.com/rkennedy/nblog_test"
)

// MockWriter is a writer that discards its input and instead merely counts the calls to its Write method.
type MockWriter struct {
	WriteCallCount uint
}

func (mw *MockWriter) Write(p []byte) (int, error) {
	mw.WriteCallCount++
	return len(p), nil
}

// TestAtomicOutput checks how many times the log handler writes to its output buffer for each log message. It should be
// _once_ to support writers that perform logic between calls to Write. For example, natefinch/lumberjack checks the
// future log size before each call to Write, which could result in a log message being split across multiple files.
// Besides, this is also the documented behavior from [slog.TextHandler.Handle].
//
// Subsequent tests using the [LineBuffer] implementation of [io.Writer] rely on this atomic behavior to inspect the
// output.
func TestAtomicOutput(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &MockWriter{}
	h := nblog.New(output)
	logger := slog.New(h)

	logger.Info("a message", slog.String("attr", "value"))
	logger.Warn("another message")
	g.Expect(output.WriteCallCount).To(Equal(uint(2)), "number of calls to Write")
}

type LineBuffer struct {
	Lines []string
}

var _ io.Writer = &LineBuffer{}

func (lb *LineBuffer) Write(b []byte) (int, error) {
	lb.Lines = append(lb.Lines, strings.TrimSuffix(string(b), "\n"))
	return len(b), nil
}

func TestBasicLogFormat(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &strings.Builder{}
	h := nblog.New(output,
		nblog.Level(slog.LevelDebug),
	)
	logger := slog.New(h)

	logger.Debug("a message")
	logger.Info("a message")
	logger.Warn("a message")
	logger.Error("a message")

	g.Expect(output.String()).To(MatchRegexp(heredoc.Doc(`
		^%[1]s \[%[2]s\] <DEBUG> %[3]s: a message
		%[1]s \[%[2]s\] <INFO> %[3]s: a message
		%[1]s \[%[2]s\] <WARN> %[3]s: a message
		%[1]s \[%[2]s\] <ERROR> %[3]s: a message
		`),
		FullTimestampRegex,
		PidRegex,
		`[^:]*`,
	))
}

func TestAttributeTypes(t *testing.T) {
	t.Parallel()

	attrs := []struct {
		slog.Attr
		Expected string
	}{
		{slog.Bool("true", true), `"true": true`},
		{slog.Bool("false", false), `"false": false`},
		{slog.Duration("duration", 5*time.Minute), `"duration": "5m0s"`},
		{slog.Float64("float64", 2.25), `"float64": 2.25`},
		{slog.Int("int", 234), `"int": 234`},
		{slog.Int64("int64", -5000000000), `"int64": -5000000000`},
		{slog.String("string", "some value"), `"string": "some value"`},
		// The label "time" is reserved; it matches slog.TimeKey.
		{slog.Time("Time", time.Date(2010, time.June, 4, 10, 34, 23, 14983, time.UTC)),
			`"Time": "2010-06-04 10:34:23.000014983 +0000 UTC"`},
		{slog.Uint64("uint64", 6000000000), `"uint64": 6000000000`},
	}

	for _, pair := range attrs {
		pair := pair
		t.Run(pair.Attr.Key, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			output := &LineBuffer{}
			h := nblog.New(output)
			logger := slog.New(h)

			logger.Info("A", pair.Attr)

			g.Expect(output.Lines[0]).To(HaveSuffix(`A {%s}`, pair.Expected))
		})
	}
}

func TestTimestampFormat(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &LineBuffer{}
	h := nblog.New(output,
		nblog.TimestampFormat(nblog.TimeOnlyFormat),
	)
	logger := slog.New(h)

	logger.Info("a message")

	g.Expect(output.Lines[0]).To(MatchRegexp(`^%[1]s \[.*a message`, TimeOnlyRegex))
}

func TestConstantLevelFiltering(t *testing.T) {
	t.Parallel()

	levels := []struct {
		slog.Level
		Matchers []any // Really types.GomegaMatcher
	}{
		{slog.LevelDebug, []any{
			ContainSubstring("<DEBUG>"),
			ContainSubstring("<INFO>"),
			ContainSubstring("<WARN>"),
			ContainSubstring("<ERROR>"),
		}},
		{slog.LevelInfo, []any{
			ContainSubstring("<INFO>"),
			ContainSubstring("<WARN>"),
			ContainSubstring("<ERROR>"),
		}},
		{slog.LevelWarn, []any{
			ContainSubstring("<WARN>"),
			ContainSubstring("<ERROR>"),
		}},
		{slog.LevelError, []any{
			ContainSubstring("<ERROR>"),
		}},
	}

	for _, lev := range levels {
		lev := lev

		t.Run(lev.Level.String(), func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			output := &LineBuffer{}
			h := nblog.New(output,
				nblog.Level(lev.Level),
			)
			logger := slog.New(h)

			logger.Debug("one")
			logger.Info("two")
			logger.Warn("three")
			logger.Error("four")

			g.Expect(output.Lines).To(HaveExactElements(lev.Matchers...))
		})
	}
}

func TestChangedLevelFiltering(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &LineBuffer{}
	var level slog.LevelVar
	h := nblog.New(output,
		nblog.Level(&level),
	)
	logger := slog.New(h)

	logger.Debug("hidden", slog.Int("line", 1))
	logger.Info("shown", slog.Int("line", 2))
	level.Set(slog.LevelDebug)
	logger.Debug("shown", slog.Int("line", 3))
	level.Set(slog.LevelError)
	logger.Debug("hidden", slog.Int("line", 4))
	logger.Info("hidden", slog.Int("line", 5))
	logger.Warn("hidden", slog.Int("line", 6))
	logger.Error("shown", slog.Int("line", 7))

	g.Expect(output.Lines).To(HaveEach(And(
		ContainSubstring("shown"),
		Not(ContainSubstring("hidden")),
	)))
}

func TestOmitCallerPackage(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &LineBuffer{}
	h := nblog.New(output,
		nblog.UseFullCallerName(false),
	)
	logger := slog.New(h)

	logger.Info("message")

	g.Expect(output.Lines[0]).To(And(
		Not(ContainSubstring(ThisPackage)),
		ContainSubstring(" TestOmitCallerPackage:"),
	))
}

func TestIncludeCallerPackage(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &LineBuffer{}
	h := nblog.New(output,
		nblog.UseFullCallerName(true),
	)
	logger := slog.New(h)

	logger.Info("message")

	g.Expect(output.Lines[0]).To(And(
		ContainSubstring(ThisPackage),
		ContainSubstring(".TestIncludeCallerPackage:"),
	))
}

func TestRenameAttr(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	repl := func(_ /* groups */ []string, attr slog.Attr) slog.Attr {
		if attr.Key == "a" {
			return slog.Attr{
				Key:   "aa",
				Value: attr.Value,
			}
		}
		return attr
	}
	output := &LineBuffer{}
	h := nblog.New(output,
		nblog.ReplaceAttr(repl),
	)
	logger := slog.New(h)

	logger.Info("message", slog.Int("a", 5), slog.Bool("b", true))
	logger.With(slog.Int("a", 5), slog.Bool("b", true)).Info("message")

	g.Expect(output.Lines).To(HaveEach(
		HaveSuffix(`message {"aa": 5, "b": true}`),
	))
}

func TestRemoveAttr(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	repl := func(_ /* groups */ []string, attr slog.Attr) slog.Attr {
		if attr.Key == "a" {
			return slog.Attr{}
		}
		return attr
	}
	output := &LineBuffer{}
	h := nblog.New(output,
		nblog.ReplaceAttr(repl),
	)
	logger := slog.New(h)

	logger.Info("message", slog.Int("a", 5), slog.Bool("b", true))
	logger.With(slog.Int("a", 5), slog.Bool("b", true)).Info("message")

	g.Expect(output.Lines).To(HaveEach(
		HaveSuffix(`message {"b": true}`),
	))
}

// TestReplaceTimeField tests replacement of the timestamp field in log records. It defines a replacement function that
// looks for the special timestamp sentinel and returns an attribute with the replacement value. The timestamp field
// comes first in the output message, so the test checks that the output has the expected value as a _prefix_.
//
//revive:disable-next-line:cognitive-complexity
func TestReplaceTimeField(t *testing.T) {
	t.Parallel()
	replacements := []struct {
		Name           string
		Replacement    slog.Attr
		ExpectedPrefix string
	}{
		// Log timestamp is replaced with time of moon landing.
		{"withTime", slog.Time("test", time.Date(1969, time.July, 20, 20, 17, 0, 0, time.UTC)),
			"1969-07-20 20:17:00.000 ["},
		// Log timestamp is replaced with other text.
		{"withOtherValue", slog.String("test", "replacement time"), "replacement time ["},
		// Log timestamp is omitted entirely.
		{"withEmptyKey", slog.Any("", nil), "["},
	}
	for _, repl := range replacements {
		t.Run(repl.Name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			output := &LineBuffer{}
			h := nblog.New(output,
				nblog.ReplaceAttr(func(groups []string, attr slog.Attr) slog.Attr {
					if len(groups) == 0 && attr.Key == slog.TimeKey {
						return repl.Replacement
					}
					return attr
				}),
			)
			logger := slog.New(h)

			logger.Info("message")

			g.Expect(output.Lines[0]).To(HavePrefix(repl.ExpectedPrefix))
		})
	}
}

func TestReplaceGroupNames(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	receivedGroups := map[string][]string{}
	repl := func(groups []string, attr slog.Attr) slog.Attr {
		receivedGroups[attr.Key] = groups
		return attr
	}
	output := &LineBuffer{}
	h := nblog.New(output,
		nblog.ReplaceAttr(repl),
	)
	logger := slog.New(h)

	logger.Info("message", slog.Group("a", slog.Int("b", 1), slog.Group("c", slog.Bool("d", false))))

	g.Expect(receivedGroups).To(And(
		HaveKeyWithValue("b", []string{"a"}),
		HaveKeyWithValue("d", []string{"a", "c"}),
	))
}

func TestNumericSeverity(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &LineBuffer{}
	h := nblog.New(output,
		nblog.Level(slog.LevelDebug),
		nblog.NumericSeverity(true),
	)
	logger := slog.New(h)

	logger.Debug("debug")
	logger.Info("info")
	logger.Warn("warn")
	logger.Error("error")

	g.Expect(output.Lines).To(HaveExactElements(
		ContainSubstring(" <2> "),
		ContainSubstring(" <4> "),
		ContainSubstring(" <8> "),
		ContainSubstring(" <16> "),
	))
}

//revive:disable-next-line:cognitive-complexity Parsing logs is complicated.
func TestLegacy(t *testing.T) {
	t.Parallel()

	formats := map[string]string{
		"full-timestamp": nblog.FullDateFormat,
		"time-only":      nblog.TimeOnlyFormat,
	}

	for label, format := range formats {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			newHandler := func(*testing.T) slog.Handler {
				buf.Reset()
				return nblog.New(&buf,
					nblog.TimestampFormat(format),
				)
			}

			parse := func(t *testing.T) map[string]any {
				// 2024-11-22 15:00:07.398 [pid] <INFO> fn: msg {"G": {"a": "v1", "b": "v2"}}
				line := buf.String()
				t.Logf("Parsing log message %#v", line)
				if line[len(line)-1] == '\n' {
					line = line[:len(line)-1]
				}

				result := make(map[string]any)

				pidIndex := strings.Index(line, " [")
				if pidIndex >= 0 {
					timestamp := line[0:pidIndex]
					logtime, err := time.Parse(format, timestamp)
					if err != nil {
						t.Logf("Could not parse date from message: %v", err)
						// Assume there is no date.
					} else {
						result[slog.TimeKey] = logtime
						line = line[pidIndex+1:]
					}
				}

				dataIndex := strings.Index(line, " {")
				if dataIndex > 0 {
					// Read additional data
					err := json.Unmarshal([]byte(line[dataIndex+1:]), &result)
					if err != nil {
						t.Errorf("Could not parse data component: %s", err.Error())
					}
					line = line[:dataIndex]
				}

				// message never contains a space during testing.
				components := strings.Split(line, " ")
				switch len(components) {
				case 3, // [pid] <LEVEL> msg
					4: // [pid] <LEVEL> fn: msg
				default:
					t.Fatalf("Expected 4 components, got %d", len(components))
				}

				// Read process ID
				pid := components[0][1 : len(components[1])-1]
				result[nblog.PidKey] = pid

				// Read severity
				severity := components[1][1 : len(components[1])-1]
				result[slog.LevelKey] = severity

				if len(components) == 4 {
					// Read caller
					caller := components[2][0 : len(components[2])-1]
					result["who"] = caller

					// Read message
					result[slog.MessageKey] = components[3]
				} else {
					// Read message
					result[slog.MessageKey] = components[2]
				}

				t.Logf("Parsed results: %#v", result)

				return result
			}

			slogtest.Run(t, newHandler, parse)
		})
	}
}

// UniformOutput is a callback function for use with [nblog.ReplaceAttr]. It replaces the time and process-ID
// pseudo-attributes with values that will be the same on every run so that tests can check for predictable output.
func UniformOutput(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) == 0 {
		switch attr.Key {
		case slog.TimeKey:
			return slog.Time(attr.Key, time.Date(2006, time.January, 2, 15, 4, 5, 0, time.UTC))
		case nblog.PidKey:
			return slog.Int(attr.Key, 42)
		}
	}
	return attr
}
