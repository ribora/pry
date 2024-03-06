package formatter

import (
	"io"
	"time"

	"github.com/rs/zerolog"

	"gitlab.com/gorib/pry/option"
)

func Plain() io.Writer {
	return zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
		w.TimeFormat = time.RFC3339
		w.FieldsExclude = option.DefaultExcludedFields
	})
}
