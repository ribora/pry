package formatter

import (
	"io"
	"os"

	"github.com/rs/zerolog"
)

func Json() io.Writer {
	zerolog.TimestampFieldName = "@timestamp"
	return os.Stdout
}
