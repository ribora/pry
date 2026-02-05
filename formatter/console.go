package formatter

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

func Plain() io.Writer {
	return zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
		w.TimeFormat = time.RFC3339
		o, _ := os.Stdout.Stat()
		w.NoColor = o.Mode()&os.ModeCharDevice == 0
	})
}
