package formatter

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"

	"gitlab.com/gorib/pry/option"
)

func Plain() io.Writer {
	return zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
		w.TimeFormat = time.RFC3339
		w.FieldsExclude = option.DefaultExcludedFields
		o, _ := os.Stdout.Stat()
		w.NoColor = o.Mode()&os.ModeCharDevice == 0
	})
}
