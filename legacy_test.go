package nblog_test

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	. "github.com/onsi/gomega"
	"github.com/rkennedy/nblog"
	"golang.org/x/exp/slog"
)

const (
	FullTimestampRegex = `\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}`
	TimeOnlyRegex      = `\d{2}:\d{2}:\d{2}\.\d{3}`
	PidRegex           = `\d+`
	ThisPackage        = "github.com/rkennedy/nblog_test"
)

// MockWriter is a writer that discards its input and instead merely counts the
// calls to its Write method.
type MockWriter struct {
	WriteCallCount uint
}

func (mw *MockWriter) Write(p []byte) (int, error) {
	mw.WriteCallCount++
	return len(p), nil
}

// TestAtomicOutput checks how many times the log handler writes to its output
// buffer for each log message. It should be _once_ to support writers that
// perform logic between calls to Write. For example, natefinch/lumberjack
// checks the future log size before each call to Write, which could result in
// a log message being split acros multiple files.
//
// Subsequent tests using the [LineBuffer] implementation of [io.Writer] rely
// on this atomic behavior to inspect the output.
func TestAtomicOutput(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &MockWriter{}
	h := nblog.NewHandler(output)
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
	h := nblog.NewHandler(output, nblog.Level(slog.LevelDebug))
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

func TestAttributes(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &LineBuffer{}
	logger := slog.New(nblog.NewHandler(output))

	logger.Info("a message", "some attribute", "some value")

	attrs := strings.SplitN(output.Lines[0], "a message", 2)[1]
	g.Expect(attrs).To(Equal(` {"some attribute": "some value"}`))
}

func TestAttributeGroups(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &LineBuffer{}
	logger := slog.New(nblog.NewHandler(output))

	logger.Info("a message", "some attribute", "some value",
		slog.Group("a group", slog.Int("an int", 5), slog.Bool("a bool", true)))

	attrs := strings.SplitN(output.Lines[0], "a message", 2)[1]
	g.Expect(attrs).To(Equal(` {"some attribute": "some value", "a group": {"an int": 5, "a bool": true}}`))
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
		{slog.Time("time", time.Date(2010, time.June, 4, 10, 34, 23, 14983, time.UTC)), `"time": "2010-06-04 10:34:23.000014983 +0000 UTC"`},
		{slog.Uint64("uint64", 6000000000), `"uint64": 6000000000`},
	}

	for _, pair := range attrs {
		pair := pair
		t.Run(pair.Attr.Key, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			output := &LineBuffer{}
			logger := slog.New(nblog.NewHandler(output))

			logger.Info("A", pair.Attr)

			g.Expect(output.Lines[0]).To(HaveSuffix(`A {%s}`, pair.Expected))
		})
	}
}

func TestTimestampFormat(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &LineBuffer{}
	logger := slog.New(nblog.NewHandler(output, nblog.TimestampFormat(nblog.TimeOnlyFormat)))

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
			logger := slog.New(nblog.NewHandler(output, nblog.Level(lev.Level)))

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
	logger := slog.New(nblog.NewHandler(output, nblog.Level(&level)))

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
	logger := slog.New(nblog.NewHandler(output, nblog.UseFullCallerName(false)))

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
	logger := slog.New(nblog.NewHandler(output, nblog.UseFullCallerName(true)))

	logger.Info("message")

	g.Expect(output.Lines[0]).To(And(
		ContainSubstring(ThisPackage),
		ContainSubstring(".TestIncludeCallerPackage:"),
	))
}

func TestOverrideCallerNameImmediate(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &LineBuffer{}
	logger := slog.New(nblog.NewHandler(output, nblog.UseFullCallerName(true)))

	logger.Info("message", slog.String("who", "override"))

	g.Expect(output.Lines[0]).To(And(
		Not(ContainSubstring(ThisPackage)),
		Not(ContainSubstring(".TestOverrideCallerNameImmediate:")),
		ContainSubstring(" override: "),
		Not(ContainSubstring(`"who": "override"`)),
	))
}

func TestOverrideCallerNameWith(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &LineBuffer{}
	logger := slog.New(nblog.NewHandler(output, nblog.UseFullCallerName(true)))

	logger = logger.With("who", "override")
	logger.Info("message")

	g.Expect(output.Lines[0]).To(And(
		Not(ContainSubstring(ThisPackage)),
		Not(ContainSubstring(".TestOverrideCallerNameWith:")),
		ContainSubstring(" override: "),
		Not(ContainSubstring(`"who": "override"`)),
	))
}

// TestWithGroup checks that multiple nested levels of
// [golang.org/x/exp/slog.Logger.WithGroup] appear correctly in the output.
func TestWithGroup(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &LineBuffer{}
	logger := slog.New(nblog.NewHandler(output))

	logger = logger.With("c", 3).WithGroup("g").With("a", 1).WithGroup("h")
	logger.Info("message", slog.Int("b", 1))

	g.Expect(output.Lines[0]).To(HaveSuffix(`: message {"c": 3, "g": {"a": 1, "h": {"b": 1}}}`))
}

// TestEmptyGroups checks that groups added by WithGroup will appear in the
// output even when they end up containing no attributes, but that groups added
// with [golang.org/x/exp/slog.Group] will be omitted when the contain no
// attributes. This is for consistency with
// [golang.org/x/exp/slog.JSONHandler].
func TestEmptyGroups(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &LineBuffer{}
	logger := slog.New(nblog.NewHandler(output))

	logger.With(slog.Int("a", 1)).WithGroup("u").Info("message")
	logger.With(slog.Int("a", 1), slog.Group("r")).Info("message")
	logger.With(slog.Int("a", 1)).Info("message", slog.Group("s"))

	g.Expect(output.Lines).To(HaveExactElements(
		HaveSuffix(`: message {"a": 1, "u": {}}`),
		HaveSuffix(`: message {"a": 1}`),
		HaveSuffix(`: message {"a": 1}`),
	))
}

func TestRenameAttr(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	repl := func(groups []string, attr slog.Attr) slog.Attr {
		if attr.Key == "a" {
			return slog.Attr{
				Key:   "aa",
				Value: attr.Value,
			}
		}
		return attr
	}
	output := &LineBuffer{}
	logger := slog.New(nblog.NewHandler(output, nblog.ReplaceAttrs(repl)))

	logger.Info("message", slog.Int("a", 5), slog.Bool("b", true))
	logger.With(slog.Int("a", 5), slog.Bool("b", true)).Info("message")

	g.Expect(output.Lines).To(HaveEach(
		HaveSuffix(`message {"aa": 5, "b": true}`),
	))
}

func TestRemoveAttr(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	repl := func(groups []string, attr slog.Attr) slog.Attr {
		if attr.Key == "a" {
			return slog.Attr{}
		}
		return attr
	}
	output := &LineBuffer{}
	logger := slog.New(nblog.NewHandler(output, nblog.ReplaceAttrs(repl)))

	logger.Info("message", slog.Int("a", 5), slog.Bool("b", true))
	logger.With(slog.Int("a", 5), slog.Bool("b", true)).Info("message")

	g.Expect(output.Lines).To(HaveEach(
		HaveSuffix(`message {"b": true}`),
	))
}

func TestReplaceTimeField(t *testing.T) {
	t.Parallel()
	replacements := []struct {
		Name           string
		Replacement    slog.Attr
		ExpectedPrefix string
	}{
		// Log timestamp is replaced with time of moon landing.
		{"withTime", slog.Time("test", time.Date(1969, time.July, 20, 20, 17, 0, 0, time.UTC)), "1969-07-20 20:17:00.000 ["},
		// Log timestamp is replaced with other text.
		{"withOtherValue", slog.String("test", "replacement time"), "replacement time ["},
		// Log timestamp is omitted entirely.
		{"withEmptyKey", slog.Any("", nil), "["},
	}
	for _, repl := range replacements {
		repl := repl
		t.Run(repl.Name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			output := &LineBuffer{}
			logger := slog.New(nblog.NewHandler(output, nblog.ReplaceAttrs(func(groups []string, attr slog.Attr) slog.Attr {
				if len(groups) == 0 && attr.Key == nblog.TimeKey {
					return repl.Replacement
				}
				return attr
			})))

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
	logger := slog.New(nblog.NewHandler(output, nblog.ReplaceAttrs(repl)))

	logger.Info("message", slog.Group("a", slog.Int("b", 1), slog.Group("c", slog.Bool("d", false))))

	g.Expect(receivedGroups).To(And(
		HaveKeyWithValue("b", []string{"a"}),
		HaveKeyWithValue("d", []string{"a", "c"}),
	))
}

func TestChainingReplacements(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &LineBuffer{}
	logger := slog.New(nblog.NewHandler(output,
		nblog.ReplaceAttrs(func(g []string, attr slog.Attr) slog.Attr {
			if attr.Key == "a" {
				attr.Value = slog.Int64Value(3 + attr.Value.Int64())
			}
			return attr
		}),
		nblog.ReplaceAttrs(func(g []string, attr slog.Attr) slog.Attr {
			if attr.Key == "a" || attr.Key == "b" {
				attr.Value = slog.Int64Value(2 * attr.Value.Int64())
			}
			return attr
		}),
	))

	logger.Info("message", "a", 1, "b", 2)

	g.Expect(output.Lines[0]).To(HaveSuffix(` message {"a": 8, "b": 4}`))
}

func TestNumericSeverity(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &LineBuffer{}
	logger := slog.New(nblog.NewHandler(output,
		nblog.Level(slog.LevelDebug),
		nblog.NumericSeverity(),
	))

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

// UniformOutput is a callback function for use with [ReplaceAttrs]. It
// replaces the time and process-ID pseudo-attributes with values that will be
// the same on every run so that tests can check for predictable output.
func UniformOutput(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) == 0 {
		switch attr.Key {
		case nblog.TimeKey:
			return slog.Time(attr.Key, time.Date(2006, time.January, 2, 15, 4, 5, 0, time.UTC))
		case nblog.PidKey:
			return slog.Int(attr.Key, 42)
		}
	}
	return attr
}
