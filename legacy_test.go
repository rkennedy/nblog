package nblog_test

import (
	"strings"
	"testing"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/rkennedy/nblog"
	"golang.org/x/exp/slog"
)

const (
	FullTimestampRegex = `\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}`
	TimeOnlyRegex      = `\d{2}:\d{2}:\d{2}\.\d{3}`
	PidRegex           = `\d+`
	ThisPackage        = "github.com/rkennedy/nblog_test"
)

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

	output := &strings.Builder{}
	h := nblog.NewHandler(output, nblog.Level(slog.LevelDebug))
	logger := slog.New(h)

	logger.Debug("a message", "some attribute", "some value")

	attrs := strings.SplitN(output.String(), "a message", 2)[1]
	g.Expect(attrs).To(Equal(" {\"some attribute\": \"some value\"}\n"))
}

func TestAttributeGroups(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &strings.Builder{}
	h := nblog.NewHandler(output, nblog.Level(slog.LevelDebug))
	logger := slog.New(h)

	logger.Debug("a message", "some attribute", "some value",
		slog.Group("a group", slog.Int("an int", 5), slog.Bool("a bool", true)))

	attrs := strings.SplitN(output.String(), "a message", 2)[1]
	g.Expect(attrs).To(Equal(` {"some attribute": "some value", "a group": {"an int": 5, "a bool": true}}
`))
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

			output := &strings.Builder{}
			logger := slog.New(nblog.NewHandler(output))

			logger.Info("A", pair.Attr)

			g.Expect(output.String()).To(HaveSuffix("A {%s}\n", pair.Expected))
		})
	}
}

func TestTimestampFormat(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &strings.Builder{}
	h := nblog.NewHandler(output, nblog.TimestampFormat(nblog.TimeOnlyFormat))
	logger := slog.New(h)

	logger.Info("a message")

	g.Expect(output.String()).To(MatchRegexp(heredoc.Doc(`
		^%[1]s \[.*a message
		`),
		TimeOnlyRegex,
	))
}

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

func TestConstantLevelFiltering(t *testing.T) {
	t.Parallel()

	levels := []struct {
		slog.Level
		Matcher types.GomegaMatcher
	}{
		{slog.LevelDebug, And(
			ContainSubstring("<DEBUG>"),
			ContainSubstring("<INFO>"),
			ContainSubstring("<WARN>"),
			ContainSubstring("<ERROR>"),
		)},
		{slog.LevelInfo, And(
			Not(ContainSubstring("<DEBUG>")),
			ContainSubstring("<INFO>"),
			ContainSubstring("<WARN>"),
			ContainSubstring("<ERROR>"),
		)},
		{slog.LevelWarn, And(
			Not(ContainSubstring("<DEBUG>")),
			Not(ContainSubstring("<INFO>")),
			ContainSubstring("<WARN>"),
			ContainSubstring("<ERROR>"),
		)},
		{slog.LevelError, And(
			Not(ContainSubstring("<DEBUG>")),
			Not(ContainSubstring("<INFO>")),
			Not(ContainSubstring("<WARN>")),
			ContainSubstring("<ERROR>"),
		)},
	}

	for _, lev := range levels {
		lev := lev

		t.Run(lev.Level.String(), func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			output := &strings.Builder{}
			logger := slog.New(nblog.NewHandler(output, nblog.Level(lev.Level)))

			logger.Debug("one")
			logger.Info("two")
			logger.Warn("three")
			logger.Error("four")

			g.Expect(output.String()).To(lev.Matcher)
		})
	}
}

func TestChangedLevelFiltering(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &strings.Builder{}
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

	g.Expect(output.String()).NotTo(ContainSubstring("hidden"))
}

func TestOmitCallerPackage(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &strings.Builder{}
	logger := slog.New(nblog.NewHandler(output, nblog.UseFullCallerName(false)))

	logger.Info("message")

	g.Expect(output.String()).To(And(
		Not(ContainSubstring(ThisPackage)),
		ContainSubstring(" TestOmitCallerPackage:"),
	))
}

func TestIncludeCallerPackage(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &strings.Builder{}
	logger := slog.New(nblog.NewHandler(output, nblog.UseFullCallerName(true)))

	logger.Info("message")

	g.Expect(output.String()).To(And(
		ContainSubstring(ThisPackage),
		ContainSubstring(".TestIncludeCallerPackage:"),
	))
}

func TestOverrideCallerNameImmediate(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &strings.Builder{}
	logger := slog.New(nblog.NewHandler(output, nblog.UseFullCallerName(true)))

	logger.Info("message", slog.String("who", "override"))

	g.Expect(output.String()).To(And(
		Not(ContainSubstring(ThisPackage)),
		Not(ContainSubstring(".TestOverrideCallerNameImmediate:")),
		ContainSubstring(" override: "),
		Not(ContainSubstring(`"who": "override"`)),
	))
}

func TestOverrideCallerNameWith(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &strings.Builder{}
	logger := slog.New(nblog.NewHandler(output, nblog.UseFullCallerName(true)))

	logger = logger.With("who", "override")
	logger.Info("message")

	g.Expect(output.String()).To(And(
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

	output := &strings.Builder{}
	logger := slog.New(nblog.NewHandler(output))

	logger = logger.With("c", 3).WithGroup("g").With("a", 1).WithGroup("h")
	logger.Info("message", slog.Int("b", 1))

	g.Expect(output.String()).To(HaveSuffix(`: message {"c": 3, "g": {"a": 1, "h": {"b": 1}}}
`))
}

// TestEmptyWithGroup checks that groups added by WithGroup will appear in the
// output even when they end up containing no attributes. This is for
// consistency with [golang.org/x/exp/slog.JSONHandler]. Groups added with
// [golang.org/x/exp/slog.Group] are ommited when empty, as tested in
// [TestEmptyGroup].
func TestEmptyWithGroup(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &strings.Builder{}
	logger := slog.New(nblog.NewHandler(output))

	logger.With(slog.Int("a", 1)).WithGroup("u").Info("message")

	g.Expect(output.String()).To(HaveSuffix(`: message {"a": 1, "u": {}}
`))
}

// TestEmptyGroup checks that groups added by [golang.org/x/exp/slog.Group] are
// omitted from the output when they're empty. This is consistent with
// [golang.org/x/exp/slog.JSONHandler].
func TestEmptyGroup(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &strings.Builder{}
	logger := slog.New(nblog.NewHandler(output))

	logger.With(slog.Int("a", 1), slog.Group("r")).Info("message")

	g.Expect(output.String()).To(HaveSuffix(`: message {"a": 1}
`))
}
