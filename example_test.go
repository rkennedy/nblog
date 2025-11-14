package nblog_test

//revive:disable:add-constant

import (
	"log/slog"
	"os"
	"time"

	"github.com/rkennedy/nblog"
)

func ExampleLevel() {
	handler := nblog.New(os.Stdout, &nblog.HandlerOptions{
		Level:       slog.LevelWarn,
		ReplaceAttr: UniformOutput,
	})
	logger := slog.New(handler)
	logger.Info("info message")
	logger.Warn("warning message")
	// Output: 2006-01-02 15:04:05.000 [42] <WARN> ExampleLevel: warning message
}

func ExampleTimestampFormat() {
	handler := nblog.New(os.Stdout, &nblog.HandlerOptions{
		TimestampFormat: time.RFC850,
		ReplaceAttr:     UniformOutput,
	})
	logger := slog.New(handler)
	logger.Info("info message")
	// Output: Monday, 02-Jan-06 15:04:05 UTC [42] <INFO> ExampleTimestampFormat: info message
}

func ExampleUseFullCallerName_true() {
	handler := nblog.New(os.Stdout, &nblog.HandlerOptions{
		UseFullCallerName: true,
		ReplaceAttr:       UniformOutput,
	})
	logger := slog.New(handler)
	logger.Info("info")
	// Output: 2006-01-02 15:04:05.000 [42] <INFO> github.com/rkennedy/nblog_test.ExampleUseFullCallerName_true: info
}

func ExampleUseFullCallerName_false() {
	handler := nblog.New(os.Stdout, &nblog.HandlerOptions{
		UseFullCallerName: false,
		ReplaceAttr:       UniformOutput,
	})
	logger := slog.New(handler)
	logger.Info("info message")
	// Output: 2006-01-02 15:04:05.000 [42] <INFO> ExampleUseFullCallerName_false: info message
}

func ExampleTrace() {
	logger := slog.New(nblog.New(os.Stdout, &nblog.HandlerOptions{
		Level:       slog.LevelDebug,
		ReplaceAttr: UniformOutput,
	}))
	defer nblog.Trace(logger).Stop()
	logger.Info("message")
}
