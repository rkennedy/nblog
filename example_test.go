package nblog_test

import (
	"os"
	"time"

	"github.com/rkennedy/nblog"
	"golang.org/x/exp/slog"
)

func ExampleLevel() {
	handler := nblog.NewHandler(os.Stdout,
		nblog.Level(slog.LevelWarn),
		nblog.ReplaceAttrs(UniformOutput),
	)
	logger := slog.New(handler)
	logger.Info("info message")
	logger.Warn("warning message")
	// Output: 2006-01-02 15:04:05.000 [42] <WARN> ExampleLevel: warning message
}

func ExampleTimestampFormat() {
	handler := nblog.NewHandler(os.Stdout,
		nblog.TimestampFormat(time.RFC850),
		nblog.ReplaceAttrs(UniformOutput),
	)
	logger := slog.New(handler)
	logger.Info("info message")
	// Output: Monday, 02-Jan-06 15:04:05 UTC [42] <INFO> ExampleTimestampFormat: info message
}

func ExampleUseFullCallerName_true() {
	handler := nblog.NewHandler(os.Stdout,
		nblog.UseFullCallerName(true),
		nblog.ReplaceAttrs(UniformOutput),
	)
	logger := slog.New(handler)
	logger.Info("info message")
	// Output: 2006-01-02 15:04:05.000 [42] <INFO> github.com/rkennedy/nblog_test.ExampleUseFullCallerName_true: info message
}

func ExampleUseFullCallerName_false() {
	handler := nblog.NewHandler(os.Stdout,
		nblog.UseFullCallerName(false),
		nblog.ReplaceAttrs(UniformOutput),
	)
	logger := slog.New(handler)
	logger.Info("info message")
	// Output: 2006-01-02 15:04:05.000 [42] <INFO> ExampleUseFullCallerName_false: info message
}
