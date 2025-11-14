package nblog_test

//revive:disable:add-constant

import (
	"log/slog"
	"os"
	"time"

	"github.com/rkennedy/nblog"
)

func ExampleHandlerOptions_Level() {
	handler := nblog.New(os.Stdout, &nblog.HandlerOptions{
		Level:       slog.LevelWarn,
		ReplaceAttr: UniformOutput,
	})
	logger := slog.New(handler)
	logger.Info("info message")
	logger.Warn("warning message")
	// Output: 2006-01-02 15:04:05.000 [42] <WARN> ExampleHandlerOptions_Level: warning message
}

func ExampleHandlerOptions_TimestampFormat() {
	handler := nblog.New(os.Stdout, &nblog.HandlerOptions{
		TimestampFormat: time.RFC850,
		ReplaceAttr:     UniformOutput,
	})
	logger := slog.New(handler)
	logger.Info("info message")
	// Output: Monday, 02-Jan-06 15:04:05 UTC [42] <INFO> ExampleHandlerOptions_TimestampFormat: info message
}

func ExampleHandlerOptions_UseFullCallerName_true() {
	handler := nblog.New(os.Stdout, &nblog.HandlerOptions{
		UseFullCallerName: true,
		ReplaceAttr:       UniformOutput,
	})
	logger := slog.New(handler)
	logger.Info("info")
	// Output: 2006-01-02 15:04:05.000 [42] <INFO> github.com/rkennedy/nblog_test.ExampleHandlerOptions_UseFullCallerName_true: info
}

func ExampleHandlerOptions_UseFullCallerName_false() {
	handler := nblog.New(os.Stdout, &nblog.HandlerOptions{
		UseFullCallerName: false,
		ReplaceAttr:       UniformOutput,
	})
	logger := slog.New(handler)
	logger.Info("info message")
	// Output: 2006-01-02 15:04:05.000 [42] <INFO> ExampleHandlerOptions_UseFullCallerName_false: info message
}

func ExampleTrace() {
	logger := slog.New(nblog.New(os.Stdout, &nblog.HandlerOptions{
		Level:       slog.LevelDebug,
		ReplaceAttr: UniformOutput,
	}))
	defer nblog.Trace(logger).Stop()
	logger.Info("message")
}
