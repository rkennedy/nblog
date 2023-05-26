//go:build ignore

// This file invokes the build. See Magefile.go for details.
// Usage:
//
//	go run mage.go
package main

import (
	"os"

	"github.com/magefile/mage/mage"
)

func main() {
	os.Exit(mage.Main())
}
