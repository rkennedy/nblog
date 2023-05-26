//go:build tools

// Package tools lists packages that are used for support tools, such as for use during a build. Using them here ensures
// that they're included in go.mod, which means that the preferred module version is tracked, and that allows "go
// install" to consistently fetch the right version without being explicitly told. In turn, that allows Magefile to
// check whether the currently installed version matches the one from go.mod and install the right one.
package tools

//revive:disable:blank-imports Blank imports are the entire point of this file.
import (
	_ "github.com/mgechev/revive"
	_ "golang.org/x/tools/cmd/goimports"
)
