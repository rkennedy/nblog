package nblog_test

import (
	"regexp"
	"strings"
	"testing"

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
		regexp.QuoteMeta(ThisPackage+".TestBasicLogFormat")))
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
	// TODO Test constant level filter
}

func TestChangedLevelFiltering(t *testing.T) {
	// TODO Test variable level filter
}
