# nblog

[![Go Reference](https://pkg.go.dev/badge/github.com/rkennedy/nblog.svg)](https://pkg.go.dev/github.com/rkennedy/nblog)

The _nblog_ package provides a handler for the _log/slog_ package that formats records in the style of NetBackup "legacy" logs:

    time [pid] <sev> caller: message

Additional attributes will appear JSON-style after the message.

# Usage

```bash
go get github.com/rkennedy/nblog
```

```go
package main

import (
	"log/slog"
	"os"

	"github.com/rkennedy/nblog"
)

func main() {
	logger := slog.New(nblog.New(os.Stdout))
	logger.Info("message")
}
```
