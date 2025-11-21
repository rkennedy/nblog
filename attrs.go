package nblog

import (
	"log/slog"
)

// ReplaceAttrFunc is the type of callback used with [ReplaceAttrs] to allow editing, replacing, or removing of
// attributes nefore a log record is recorded. The function will be called for each non-group attribute, along with a
// list of the currently nested groups. The function can return the original attribute to log it as-is, return a
// different attribute to use it instead, or return an attribute with an empty Key value to omit the current attribute
// entirely.
type ReplaceAttrFunc func(groups []string, attr slog.Attr) slog.Attr

// ChainReplace combines two attribute-replacement functions to call them in sequence, passing the result of one as the
// input to the next. If one of the input function pointers is null, then this simply returns the other function
// directly. If they're both null, then this returns the identity function.
func ChainReplace(repl1, repl2 ReplaceAttrFunc) ReplaceAttrFunc {
	if repl1 == nil {
		if repl2 == nil {
			return func(_ /* groups */ []string, attr slog.Attr) slog.Attr {
				return attr
			}
		}
		return repl2
	}
	if repl2 == nil {
		return repl1
	}
	return func(groups []string, a slog.Attr) slog.Attr {
		return repl2(groups, repl1(groups, a))
	}
}
