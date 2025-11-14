package nblog_test

import (
	"log/slog"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/rkennedy/nblog"
)

func DoTrace(logger *slog.Logger) {
	defer nblog.Trace(logger).Stop()
}

func TestTrace(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &LineBuffer{}
	logger := slog.New(nblog.New(output, &nblog.HandlerOptions{Level: slog.LevelDebug}))

	DoTrace(logger)

	g.Expect(output.Lines).To(HaveExactElements(
		HaveSuffix(`<DEBUG> DoTrace: Entered.`),
		ContainSubstring(`<DEBUG> DoTrace: Exited. {"duration": "`),
	))
}
