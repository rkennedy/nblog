package nblog_test

import (
	"testing"

	. "github.com/onsi/gomega"
	"github.com/rkennedy/nblog"
	"golang.org/x/exp/slog"
)

func DoTrace(logger *slog.Logger) {
	defer nblog.Trace(logger).Stop()
}

func TestTrace(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	output := &LineBuffer{}
	logger := slog.New(nblog.NewHandler(output, nblog.Level(slog.LevelDebug)))

	DoTrace(logger)

	g.Expect(output.Lines).To(HaveExactElements(
		HaveSuffix(`<DEBUG> DoTrace: Entered.`),
		ContainSubstring(`<DEBUG> DoTrace: Exited. {"duration": "`),
	))
}
