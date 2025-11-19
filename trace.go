package nblog

import (
	"context"
	"log/slog"
	"runtime"
	"time"
)

// TraceStopper is the interface returned by [Trace] to allow callers to stop the trace. Use it with defer. For example:
//
//	defer nblog.Trace(logger).Stop()
type TraceStopper interface {
	Stop()
}

type stopper struct {
	logger *slog.Logger
	pc     uintptr
	start  time.Time
}

func (s *stopper) Stop() {
	r := slog.NewRecord(time.Now(), slog.LevelDebug, "Exited.", s.pc)
	r.Add(slog.Duration("duration", time.Since(s.start)))
	_ = s.logger.Handler().Handle(context.Background(), r)
}

type nullStopper struct{}

func (*nullStopper) Stop() {}

// Trace marks the start of a function and returns a [TraceStopper] that can be used to mark the end of the function.
// Trace logs the message “Entered” to the logger. Afterward, [TraceStopper.Stop] logs the message “Exited” along with a
// “duration” attribute to indicate how long the function ran.
func Trace(logger *slog.Logger) TraceStopper {
	if !logger.Enabled(context.Background(), slog.LevelDebug) {
		return &nullStopper{}
	}
	var pcs [1]uintptr
	const callsToSkip = 2 // runtime.Callers, this function
	runtime.Callers(callsToSkip, pcs[:])
	pc := pcs[0]
	now := time.Now()
	r := slog.NewRecord(now, slog.LevelDebug, "Entered.", pc)
	_ = logger.Handler().Handle(context.Background(), r)
	return &stopper{logger, pc, now}
}
