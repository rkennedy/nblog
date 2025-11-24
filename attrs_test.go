package nblog_test

import (
	"log/slog"
	"testing"

	. "github.com/onsi/gomega"
	"sweetkennedy.net/nblog"
)

func repl1(_ /* groups */ []string, a slog.Attr) slog.Attr {
	if a.Key == "a" {
		a.Value = slog.Int64Value(3 + a.Value.Int64())
	}
	return a
}

func repl2(_ /* groups */ []string, a slog.Attr) slog.Attr {
	if a.Key == "a" || a.Key == "b" {
		a.Value = slog.Int64Value(2 * a.Value.Int64())
	}
	return a
}

func TestChainReplace(t *testing.T) {
	t.Parallel()

	repl := nblog.ChainReplace(repl1, repl2)
	t.Run("two-repl-functions", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		attr := repl([]string{}, slog.Int64("a", 1))
		g.Expect(attr.Value.Kind()).To(Equal(slog.KindInt64))
		g.Expect(attr.Value.Int64()).To(Equal(int64(8)))
	})
	t.Run("one-repl-function", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		attr := repl([]string{}, slog.Int64("b", 2))
		g.Expect(attr.Value.Kind()).To(Equal(slog.KindInt64))
		g.Expect(attr.Value.Int64()).To(Equal(int64(4)))
	})
}
