# nblog

[![Go Reference](https://pkg.go.dev/badge/sweetkennedy.net/nblog.svg)](https://pkg.go.dev/sweetkennedy.net/nblog)

The _nblog_ package provides a handler for the _log/slog_ package that formats records in the style of NetBackup "legacy" logs:

    time [pid] <sev> caller: message

Additional attributes will appear JSON-style after the message.

# Usage

```bash
go get sweetkennedy.net/nblog
```

```go
package main

import (
	"log/slog"
	"os"

	"sweetkennedy.net/nblog"
)

func main() {
	logger := slog.New(nblog.New(os.Stdout))
	logger.Info("message")
}
```
