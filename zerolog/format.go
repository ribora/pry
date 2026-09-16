package zerolog

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Plain is human-readable console output. Colour is dropped when stdout is not
// a TTY.
func Plain() io.Writer {
	return zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
		w.TimeFormat = time.RFC3339
		o, _ := os.Stdout.Stat()
		w.NoColor = o.Mode()&os.ModeCharDevice == 0
	})
}

// Json is stdout. It also sets zerolog's global timestamp field name, which
// affects every zerolog logger in the process.
func Json() io.Writer {
	zerolog.TimestampFieldName = "@timestamp"
	return os.Stdout
}
