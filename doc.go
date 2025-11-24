// Package nblog provides a handler for the [log/slog] package where output is produced in the format used by NetBackup
// legacy log files.
//
//	time [pid] <sev> caller: message
//
// Additional attributes will appear JSON-style after the message:
//
//	time [pid] <sev> caller: message {"attribute": "value"}
package nblog
